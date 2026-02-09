package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/mlctrez/goemail/sesutil"
)

type s3API interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

func extractEmail(address string) string {
	addr, err := mail.ParseAddress(address)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(address))
	}
	return strings.ToLower(addr.Address)
}

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

	if len(event.Records) == 0 {
		return nil, nil
	}

	from := os.Getenv("EMAIL_FROM")
	to := extractEmail(os.Getenv("EMAIL_TO"))
	bucket := aws.String(os.Getenv("EMAIL_BUCKET"))

	ec := sesutil.EmailContext(sesClient, from, to)
	blocks := getBlocks(ctx, s3Client, bucket)

	for _, record := range event.Records {
		sesMail := record.SES.Mail

		isBlocked := false
		for _, dest := range sesMail.Destination {
			if _, ok := blocks[extractEmail(dest)]; ok {
				isBlocked = true
				break
			}
		}

		if isBlocked {
			log.Printf("Blocking email to %v", sesMail.Destination)
			_, errDel := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: bucket,
				Delete: &s3Types.Delete{
					Objects: []s3Types.ObjectIdentifier{{Key: aws.String(sesMail.MessageID)}},
				},
			})
			if errDel != nil {
				log.Printf("s3Client.DeleteObjects error for blocked sesMail %s: %s", sesMail.MessageID, errDel)
			}
			continue
		}

		if strings.ToLower(sesMail.CommonHeaders.Subject) == "block" {
			isFromOwner := false
			if extractEmail(sesMail.Source) == to {
				isFromOwner = true
			}
			if isFromOwner {
				newBlocks := make([]string, 0)
				for _, dest := range sesMail.Destination {
					destEmail := extractEmail(dest)
					if destEmail != to {
						newBlocks = append(newBlocks, destEmail)
					}
				}
				if len(newBlocks) > 0 {
					log.Printf("Adding to block list: %v", newBlocks)
					updateBlocks(ctx, s3Client, bucket, blocks, newBlocks)
					ec.Send(fmt.Sprintf("Added to block list: %v", newBlocks))
					// Also delete the command email
					_, _ = s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
						Bucket: bucket,
						Delete: &s3Types.Delete{
							Objects: []s3Types.ObjectIdentifier{{Key: aws.String(sesMail.MessageID)}},
						},
					})
					continue
				}
			}
		}

		getObjectInput := &s3.GetObjectInput{Bucket: bucket, Key: aws.String(sesMail.MessageID)}

		var getObjectOutput *s3.GetObjectOutput
		getObjectOutput, err = s3Client.GetObject(ctx, getObjectInput)
		if err != nil {
			log.Printf("s3Client.GetObject error for %s: %s", sesMail.MessageID, err)
			ec.Send(fmt.Sprintf("s3Client.GetObject err : %s", err))
			continue
		}

		_, err = sesClient.SendRawEmail(ctx, &ses.SendRawEmailInput{
			RawMessage:   sesutil.Process(getObjectOutput.Body, from, to),
			Source:       &from,
			Destinations: []string{to},
		})
		if err != nil {
			log.Printf("sesClient.SendRawEmail error for %s: %s", sesMail.MessageID, err)
			psClient := s3.NewPresignClient(s3Client, s3.WithPresignExpires(time.Second*604800))
			psReq, psErr := psClient.PresignGetObject(ctx, getObjectInput)
			if psErr != nil {
				ec.Send(fmt.Sprintf("PresignGetObject err : %s", psErr))
				continue
			}
			ec.Send(fmt.Sprintf("RawEmail %s \r\nSendRawEmail err : %s", psReq.URL, err))
			continue
		} else {
			_, errDel := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: bucket,
				Delete: &s3Types.Delete{
					Objects: []s3Types.ObjectIdentifier{{Key: getObjectInput.Key}},
				},
			})
			if errDel != nil {
				log.Printf("s3Client.DeleteObjects error for %s: %s", sesMail.MessageID, errDel)
				ec.Send(fmt.Sprintf("DeleteObjects err : %s", errDel))
				continue
			}
		}
	}

	return nil, nil
}

const blocksKey = "blocks.txt"

func getBlocks(ctx context.Context, client s3API, bucket *string) map[string]struct{} {
	res := make(map[string]struct{})
	output, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: bucket,
		Key:    aws.String(blocksKey),
	})
	if err != nil {
		return res
	}
	defer func() { _ = output.Body.Close() }()
	body, err := io.ReadAll(output.Body)
	if err != nil {
		return res
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = extractEmail(line)
		if line != "" {
			res[line] = struct{}{}
		}
	}
	return res
}

func updateBlocks(ctx context.Context, client s3API, bucket *string, current map[string]struct{}, news []string) {
	for _, n := range news {
		current[extractEmail(n)] = struct{}{}
	}
	var sb strings.Builder
	for k := range current {
		sb.WriteString(k)
		sb.WriteString("\n")
	}
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: bucket,
		Key:    aws.String(blocksKey),
		Body:   strings.NewReader(sb.String()),
	})
	if err != nil {
		log.Printf("Error updating blocks.txt: %s", err)
	}
}
