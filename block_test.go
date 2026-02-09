package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type mockS3 struct {
	s3API
	getObjectFunc func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	putObjectFunc func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

func (m *mockS3) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return m.getObjectFunc(ctx, params, optFns...)
}

func (m *mockS3) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return m.putObjectFunc(ctx, params, optFns...)
}

func TestGetBlocks(t *testing.T) {
	mock := &mockS3{
		getObjectFunc: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			body := "shady@mlctrez.com\nspam@mlctrez.com "
			return &s3.GetObjectOutput{
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		},
	}

	blocks := getBlocks(context.Background(), mock, aws.String("bucket"))
	if _, ok := blocks["shady@mlctrez.com"]; !ok {
		t.Error("expected shady@mlctrez.com to be in blocks")
	}
	if _, ok := blocks["spam@mlctrez.com"]; !ok {
		t.Error("expected spam@mlctrez.com to be in blocks")
	}
	if len(blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(blocks))
	}
}

func TestExtractEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"shady@mlctrez.com", "shady@mlctrez.com"},
		{" Matt <matt@mlctrez.com> ", "matt@mlctrez.com"},
		{"<MATT@MLCTREZ.COM>", "matt@mlctrez.com"},
		{"Invalid Address", "invalid address"},
	}

	for _, tc := range tests {
		got := extractEmail(tc.input)
		if got != tc.expected {
			t.Errorf("extractEmail(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestUpdateBlocks(t *testing.T) {
	var putBody string
	mock := &mockS3{
		putObjectFunc: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			buf, _ := io.ReadAll(params.Body)
			putBody = string(buf)
			return &s3.PutObjectOutput{}, nil
		},
	}

	current := map[string]struct{}{
		"old@mlctrez.com": {},
	}
	updateBlocks(context.Background(), mock, aws.String("bucket"), current, []string{"new@mlctrez.com"})

	if !strings.Contains(putBody, "old@mlctrez.com") {
		t.Error("expected old@mlctrez.com in put body")
	}
	if !strings.Contains(putBody, "new@mlctrez.com") {
		t.Error("expected new@mlctrez.com in put body")
	}
}
