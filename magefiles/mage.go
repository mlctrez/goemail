package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamTypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lamTypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"
)

var Default = Deploy

type buildTarget func(context.Context) error

var _ = []buildTarget{Default, Deploy, Delete}

func Deploy(ctxIn context.Context) error {
	return newBuildContext(ctxIn).deploy()
}

func Delete(ctx context.Context) error {
	return newBuildContext(ctx).delete()
}

func (bc *buildContext) deploy() error {
	return steps(bc.envInit, bc.awsInit, bc.buildCode,
		bc.ensureRole, bc.ensureFunction, bc.updateFunctionCode)
}

func (bc *buildContext) delete() error {
	return steps(bc.envInit, bc.awsInit, bc.deleteFunction, bc.deleteRole)
}

func steps(steps ...func() error) (err error) {
	for _, step := range steps {
		fmt.Println(runtime.FuncForPC(reflect.ValueOf(step).Pointer()).Name())
		if err = step(); err != nil {
			return err
		}
	}
	return nil
}

func newBuildContext(ctx context.Context) *buildContext {
	return &buildContext{ctx: ctx}
}

type buildContext struct {
	ctx       context.Context
	fnName    string
	fnRole    string
	cfg       aws.Config
	lamClient *lambda.Client
	iamClient *iam.Client
	role      *iam.GetRoleOutput
	fn        *lambda.GetFunctionOutput
	tempDir   string
	zipBytes  []byte
}

func (bc *buildContext) envInit() (err error) {
	bc.fnName = os.Getenv("GO_LAMBDA_NAME")
	if bc.fnName == "" {
		var result []byte
		command := exec.Command("go", "list", "-m")
		if result, err = command.CombinedOutput(); err != nil {
			goCmdFailed(command, result)
			return err
		}
		bc.fnName = filepath.Base(strings.TrimSpace(string(result)))
	}
	fmt.Printf("using function name %s, override with env GO_LAMBDA_NAME\n", bc.fnName)
	bc.fnRole = fmt.Sprintf("%s_role", bc.fnName)
	return nil
}

func (bc *buildContext) awsInit() (err error) {
	if bc.cfg, err = config.LoadDefaultConfig(bc.ctx); err != nil {
		return err
	}
	bc.lamClient = lambda.NewFromConfig(bc.cfg)
	bc.iamClient = iam.NewFromConfig(bc.cfg)

	_ = iamTypes.ResourceSpecificResult{}
	_ = lamTypes.ResourceNotFoundException{}
	return nil
}

func (bc *buildContext) buildCode() (err error) {
	return steps(bc.mkTempDir, bc.goBuild, bc.buildZipBytes, bc.rmTempDir)
}

func (bc *buildContext) mkTempDir() (err error) {
	bc.tempDir, err = os.MkdirTemp(os.TempDir(), "go-lambda")
	return err
}

func (bc *buildContext) rmTempDir() (err error) {
	if bc.tempDir != "" {
		err = os.RemoveAll(bc.tempDir)
		bc.tempDir = ""
	}
	return err
}

func (bc *buildContext) goBuild() (err error) {
	lamBinary := filepath.Join(bc.tempDir, "bootstrap")
	command := exec.Command("go", "build", "-o", lamBinary, ".")
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	var result []byte
	if result, err = command.CombinedOutput(); err != nil {
		goCmdFailed(command, result)
	}
	return err
}

func goCmdFailed(cmd *exec.Cmd, result []byte) {
	fullCommand := fmt.Sprintf("go %s", strings.Join(cmd.Args, " "))
	fmt.Println("*** command failed : ", fullCommand)
	fmt.Println(string(result))
	fmt.Println("*** end command failed")
}

func (bc *buildContext) buildZipBytes() (err error) {
	zipBuf := &bytes.Buffer{}
	zipWriter := zip.NewWriter(zipBuf)
	var open *os.File
	var zfWriter io.Writer

	return steps(func() (err error) {
		open, err = os.Open(filepath.Join(bc.tempDir, "bootstrap"))
		return err
	}, func() (err error) {
		zfWriter, err = zipWriter.Create("bootstrap")
		return err
	}, func() error {
		_, err = io.Copy(zfWriter, open)
		return err
	}, zipWriter.Flush, zipWriter.Close, func() error {
		bc.zipBytes = zipBuf.Bytes()
		return nil
	})
}

func (bc *buildContext) ensureRole() (err error) {
	gri := &iam.GetRoleInput{RoleName: &bc.fnRole}
	bc.role, err = bc.iamClient.GetRole(bc.ctx, gri)
	if bc.role != nil {
		return nil
	}
	// return errors other than not exists
	var noSuchEntity *iamTypes.NoSuchEntityException
	if !errors.As(err, &noSuchEntity) {
		return err
	}

	var cro *iam.CreateRoleOutput
	cri := &iam.CreateRoleInput{
		RoleName:                 &bc.fnRole,
		Description:              aws.String("role for lambda " + bc.fnName),
		AssumeRolePolicyDocument: aws.String(lambdaAssumeRolePolicy),
	}
	if cro, err = bc.iamClient.CreateRole(bc.ctx, cri); err != nil {
		return err
	}

	// customize additional roles for the lambda here
	policies := []string{
		"arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole",
	}
	for _, policyArn := range policies {
		rpi := &iam.AttachRolePolicyInput{RoleName: cro.Role.RoleName, PolicyArn: &policyArn}
		if _, err = bc.iamClient.AttachRolePolicy(bc.ctx, rpi); err != nil {
			return err
		}
	}

	return bc.ensureRole()
}

var lambdaAssumeRolePolicy = `{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "Service": "lambda.amazonaws.com"
            },
            "Action": "sts:AssumeRole"
        }
    ]
}`

func (bc *buildContext) ensureFunction() (err error) {
	gfi := &lambda.GetFunctionInput{FunctionName: &bc.fnName}
	bc.fn, err = bc.lamClient.GetFunction(bc.ctx, gfi)
	if bc.fn != nil {
		return nil
	}
	var notFoundError *lamTypes.ResourceNotFoundException
	if !errors.As(err, &notFoundError) {
		return err
	}

	var waitForPolicy = 10
	for waitForPolicy > 0 {
		cfi := &lambda.CreateFunctionInput{
			FunctionName: &bc.fnName,
			Role:         bc.role.Role.Arn,
			Code:         &lamTypes.FunctionCode{ZipFile: bc.zipBytes},
			Handler:      aws.String("bootstrap"),
			Runtime:      lamTypes.RuntimeProvidedal2,
		}
		_, err = bc.lamClient.CreateFunction(bc.ctx, cfi)
		if err != nil {
			var ipa *lamTypes.InvalidParameterValueException
			if errors.As(err, &ipa) {
				waitForPolicy--
				time.Sleep(time.Second)
				continue
			}
			return err
		}
		waitForPolicy = 0
	}
	_, err = bc.lamClient.AddPermission(bc.ctx, &lambda.AddPermissionInput{
		Action:       aws.String("lambda:InvokeFunction"),
		FunctionName: &bc.fnName,
		Principal:    aws.String("ses.amazonaws.com"),
		StatementId:  aws.String("allowSesInvoke"),
	})
	return err
}

func (bc *buildContext) updateFunctionCode() (err error) {
	if bc.fn != nil {
		ufc := &lambda.UpdateFunctionCodeInput{FunctionName: &bc.fnName, ZipFile: bc.zipBytes}
		_, err = bc.lamClient.UpdateFunctionCode(bc.ctx, ufc)
	}
	return err
}

func (bc *buildContext) deleteFunction() (err error) {
	dfi := &lambda.DeleteFunctionInput{FunctionName: &bc.fnName}
	_, err = bc.lamClient.DeleteFunction(bc.ctx, dfi)
	return err
}

func (bc *buildContext) deleteRole() (err error) {
	var rpo *iam.ListAttachedRolePoliciesOutput
	return steps(func() (err error) {
		rpi := &iam.ListAttachedRolePoliciesInput{RoleName: &bc.fnRole}
		rpo, err = bc.iamClient.ListAttachedRolePolicies(bc.ctx, rpi)
		return err
	}, func() (err error) {
		for _, pol := range rpo.AttachedPolicies {
			dpi := &iam.DetachRolePolicyInput{RoleName: &bc.fnRole, PolicyArn: pol.PolicyArn}
			if _, err = bc.iamClient.DetachRolePolicy(bc.ctx, dpi); err != nil {
				return err
			}
		}
		return nil
	}, func() (err error) {
		dri := &iam.DeleteRoleInput{RoleName: &bc.fnRole}
		_, err = bc.iamClient.DeleteRole(bc.ctx, dri)
		return err
	})

}
