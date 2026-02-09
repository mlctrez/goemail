package sesutil

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	sesTypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
)

type emailContext struct {
	sesClient *ses.Client
	from      string
	to        string
}

func EmailContext(sesClient *ses.Client, from, to string) *emailContext {
	return &emailContext{sesClient: sesClient, from: from, to: to}
}

func (a *emailContext) Send(message string) {
	_, _ = a.sesClient.SendEmail(context.Background(), &ses.SendEmailInput{
		Source:      &a.from,
		Destination: &sesTypes.Destination{ToAddresses: []string{a.to}},
		Message: &sesTypes.Message{
			Body:    &sesTypes.Body{Text: content(message)},
			Subject: content("go email admin"),
		},
	})
}

func content(data string) *sesTypes.Content {
	return &sesTypes.Content{Data: aws.String(data), Charset: aws.String("UTF-8")}
}
