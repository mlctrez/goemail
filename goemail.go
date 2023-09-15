package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	sesTypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
	"io"
	"log"
	"strings"
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

type messagePart struct {
	first      string
	additional []string
}

var ignoreHeaders = []string{
	"dkim-", "from:", "return-path:", "to:", "sender:", "list-owner:",
}

func fromAddress() *string {
	return aws.String("Mail Man <mailman@mlctrez.com>")
}

func toAddress() *string {
	return aws.String("Matt Crawford <mlctrez@gmail.com>")
}

func (m messagePart) write(b *bytes.Buffer) {

	firstLower := strings.ToLower(m.first)
	for _, header := range ignoreHeaders {
		if strings.HasPrefix(firstLower, header) {
			switch header {
			case "from:":
				b.WriteString(fmt.Sprintf("From: %s\r\n", *fromAddress()))
			case "to:":
				b.WriteString(fmt.Sprintf("From: %s\r\n", *toAddress()))
			}
			return
		}
	}

	b.WriteString(m.first)
	b.WriteString("\r\n")
	for _, s := range m.additional {
		b.WriteString(s)
		b.WriteString("\r\n")
	}
}

func processMessage(reader io.ReadCloser) *sesTypes.RawMessage {
	defer func() { _ = reader.Close() }()
	buf := &bytes.Buffer{}
	scanner := bufio.NewScanner(reader)
	var mp *messagePart

	for scanner.Scan() {
		l := scanner.Text()
		if strings.HasPrefix(l, " ") {
			mp.additional = append(mp.additional, l)
		} else {
			if mp != nil {
				mp.write(buf)
			}
			mp = &messagePart{first: l}
		}
	}
	if mp != nil {
		mp.write(buf)
	}

	return &sesTypes.RawMessage{Data: buf.Bytes()}
}

func Handle(ctx context.Context, event events.SimpleEmailEvent) (response interface{}, err error) {
	s3Client, sesClient := clients(ctx)

	if event.Records == nil || len(event.Records) == 0 {
		return nil, nil
	}

	bucket := aws.String("mlctrez-inbound-email")
	for _, record := range event.Records {
		mail := record.SES.Mail

		getObjectInput := &s3.GetObjectInput{Bucket: bucket, Key: aws.String(mail.MessageID)}

		var getObjectOutput *s3.GetObjectOutput
		getObjectOutput, err = s3Client.GetObject(ctx, getObjectInput)
		if err != nil {
			handleError(sesClient, "go email admin", fmt.Sprintf("s3Client.GetObject err : %s", err))
			return nil, nil
		}

		_, err = sesClient.SendRawEmail(ctx, &ses.SendRawEmailInput{
			RawMessage:   processMessage(getObjectOutput.Body),
			Source:       fromAddress(),
			Destinations: []string{*toAddress()},
		})
		if err != nil {
			psClient := s3.NewPresignClient(s3Client, s3.WithPresignExpires(time.Second*604800))
			psReq, psErr := psClient.PresignGetObject(ctx, getObjectInput)
			if psErr != nil {
				handleError(sesClient, "go email admin", fmt.Sprintf("PresignGetObject err : %s", psErr))
				break
			}
			handleError(sesClient, "go email admin", fmt.Sprintf("RawEmail %s \r\nSendRawEmail err : %s", psReq.URL, err))
			break
		} else {
			_, errDel := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: bucket,
				Delete: &s3Types.Delete{
					Objects: []s3Types.ObjectIdentifier{{Key: getObjectInput.Key}},
				},
			})
			if errDel != nil {
				handleError(sesClient, "go email admin", fmt.Sprintf("DeleteObjects err : %s", errDel))
				break
			}
		}
	}

	return nil, nil
}

func handleError(sesClient *ses.Client, subject, message string) {
	_, _ = sesClient.SendEmail(context.Background(), &ses.SendEmailInput{
		Source:      fromAddress(),
		Destination: &sesTypes.Destination{ToAddresses: []string{*toAddress()}},
		Message: &sesTypes.Message{
			Body:    &sesTypes.Body{Text: content(message)},
			Subject: content(subject),
		},
	})
}

func content(data string) *sesTypes.Content {
	return &sesTypes.Content{Data: aws.String(data), Charset: aws.String("UTF-8")}
}
