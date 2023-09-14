package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamTypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lamTypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

var Default = Deploy

var cfg aws.Config
var lamClient *lambda.Client
var iamClient *iam.Client

func functionName() *string {
	return aws.String("goemail")
}

func functionRole() *string {
	return aws.String(*functionName() + "_role")
}

func awsInit(ctx context.Context) {
	var err error
	cfg, err = config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		panic(err)
	}
	lamClient = lambda.NewFromConfig(cfg)
	iamClient = iam.NewFromConfig(cfg)

	_ = iamTypes.ResourceSpecificResult{}
	_ = lamTypes.ResourceNotFoundException{}
}

func Deploy(ctx context.Context) {
	awsInit(ctx)

	if !roleExists(ctx) {
		createRole(ctx)
	}

	codeBytes, err := buildCode(ctx)
	if err != nil {
		panic(err)
	}

	if functionExists(ctx) {
		_, err = lamClient.UpdateFunctionCode(ctx, &lambda.UpdateFunctionCodeInput{
			FunctionName: functionName(),
			ZipFile:      codeBytes,
		})
		if err != nil {
			panic(err)
		}
	} else {
		_, err = lamClient.CreateFunction(ctx, &lambda.CreateFunctionInput{
			FunctionName: functionName(),
			Role:         lookupRole(ctx),
			Code:         &lamTypes.FunctionCode{ZipFile: codeBytes},
			Handler:      aws.String("bootstrap"),
			Runtime:      lamTypes.RuntimeProvidedal2,
		})
		if err != nil {
			log.Fatal(err)
		}
		_, err = lamClient.AddPermission(ctx, &lambda.AddPermissionInput{
			Action:       aws.String("lambda:InvokeFunction"),
			FunctionName: functionName(),
			Principal:    aws.String("ses.amazonaws.com"),
			StatementId:  aws.String("allowSesInvoke"),
		})
		if err != nil {
			log.Fatal(err)
		}

	}
}

func buildCode(ctx context.Context) (zipContents []byte, err error) {

	temp, err := os.MkdirTemp(os.TempDir(), "goemail")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(temp) }()

	lamBinary := filepath.Join(temp, "bootstrap")
	command := exec.Command("go", "build", "-o", lamBinary, ".")
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	_, err = command.CombinedOutput()
	if err != nil {
		return nil, err
	}
	lamBytes, err := os.ReadFile(lamBinary)
	if err != nil {
		return nil, err
	}

	zipBuf := &bytes.Buffer{}
	zipWriter := zip.NewWriter(zipBuf)
	defer func() { _ = zipWriter.Close() }()

	fileWriter, err := zipWriter.Create("bootstrap")
	if err != nil {
		return nil, err
	}
	_, err = fileWriter.Write(lamBytes)
	if err != nil {
		return nil, err
	}
	err = zipWriter.Flush()
	if err != nil {
		return nil, err
	}
	err = zipWriter.Close()
	if err != nil {
		return nil, err
	}

	return zipBuf.Bytes(), nil
}

func functionExists(ctx context.Context) bool {
	gfi := &lambda.GetFunctionInput{FunctionName: functionName()}
	if _, err := lamClient.GetFunction(ctx, gfi); err != nil {
		var notFoundError *lamTypes.ResourceNotFoundException
		if errors.As(err, &notFoundError) {
			return false
		}
		panic(err)
	}
	return true
}

func lookupRole(ctx context.Context) *string {
	role, err := iamClient.GetRole(ctx, &iam.GetRoleInput{RoleName: functionRole()})
	if err != nil {
		panic(err)
	}
	return role.Role.Arn
}

func roleExists(ctx context.Context) bool {
	_, err := iamClient.GetRole(ctx, &iam.GetRoleInput{RoleName: functionRole()})
	var noSuchEntity *iamTypes.NoSuchEntityException
	if err != nil {
		if errors.As(err, &noSuchEntity) {
			return false
		}
		panic(err)
	}
	return true
}

func createRole(ctx context.Context) {
	role, err := iamClient.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 functionRole(),
		Description:              aws.String("role for lambda " + *functionName()),
		AssumeRolePolicyDocument: aws.String(lambdaAssumeRolePolicy),
	})
	if err != nil {
		panic(err)
	}
	// TODO: narrow these
	managedRoles := []string{
		"arn:aws:iam::aws:policy/AmazonS3FullAccess",
		"arn:aws:iam::aws:policy/AmazonSESFullAccess",
		"arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole",
	}
	for _, managedRole := range managedRoles {
		_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			RoleName:  role.Role.RoleName,
			PolicyArn: aws.String(managedRole),
		})
		if err != nil {
			panic(err)
		}
	}

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
