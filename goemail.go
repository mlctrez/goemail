package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/mlctrez/goemail/sesutil"
	"log"
	"os"
	"time"
)

func main() {
	lambda.Start(Handle)
}

func clients(ctx context.Context) (s3Client *s3.Client, sesClient *ses.Client) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		log.Fatal(err)
	}
	return s3.NewFromConfig(cfg), ses.NewFromConfig(cfg)
}

func Handle(ctx context.Context, event events.SimpleEmailEvent) (response interface{}, err error) {
	s3Client, sesClient := clients(ctx)

	if event.Records == nil || len(event.Records) == 0 {
		return nil, nil
	}

	from := os.Getenv("EMAIL_FROM")
	to := os.Getenv("EMAIL_TO")
	bucket := aws.String(os.Getenv("EMAIL_BUCKET"))

	ec := sesutil.EmailContext(sesClient, from, to)

	for _, record := range event.Records {
		mail := record.SES.Mail

		getObjectInput := &s3.GetObjectInput{Bucket: bucket, Key: aws.String(mail.MessageID)}

		var getObjectOutput *s3.GetObjectOutput
		getObjectOutput, err = s3Client.GetObject(ctx, getObjectInput)
		if err != nil {
			ec.Send(fmt.Sprintf("s3Client.GetObject err : %s", err))
			return nil, nil
		}

		_, err = sesClient.SendRawEmail(ctx, &ses.SendRawEmailInput{
			RawMessage:   sesutil.Process(getObjectOutput.Body, from, to),
			Source:       &from,
			Destinations: []string{to},
		})
		if err != nil {
			psClient := s3.NewPresignClient(s3Client, s3.WithPresignExpires(time.Second*604800))
			psReq, psErr := psClient.PresignGetObject(ctx, getObjectInput)
			if psErr != nil {
				ec.Send(fmt.Sprintf("PresignGetObject err : %s", psErr))
				break
			}
			ec.Send(fmt.Sprintf("RawEmail %s \r\nSendRawEmail err : %s", psReq.URL, err))
			break
		} else {
			_, errDel := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: bucket,
				Delete: &s3Types.Delete{
					Objects: []s3Types.ObjectIdentifier{{Key: getObjectInput.Key}},
				},
			})
			if errDel != nil {
				ec.Send(fmt.Sprintf("DeleteObjects err : %s", errDel))
				break
			}
		}
	}

	return nil, nil
}
