package sesutil

import (
	"io"
	"strings"
	"testing"
)

func TestProcess(t *testing.T) {
	input := `From: sender@example.com
To: original@mlctrez.com
Subject: Test Email
Date: Mon, 08 Feb 2026 19:48:00 +0000

This is the body of the email.
To: this should not be changed.
`
	from := "forwarder@mlctrez.com"
	to := "destination@gmail.com"

	reader := io.NopCloser(strings.NewReader(input))
	rawMessage := Process(reader, from, to)
	output := string(rawMessage.Data)

	if !strings.Contains(output, "From: forwarder@mlctrez.com") {
		t.Errorf("Expected From header to be updated. Got:\n%s", output)
	}

	if !strings.Contains(output, "To: destination@gmail.com") {
		t.Errorf("Expected To header to be updated. Got:\n%s", output)
	}

	if !strings.Contains(output, "X-Original-To: original@mlctrez.com") {
		t.Errorf("Expected X-Original-To header to be present. Got:\n%s", output)
	}

	if !strings.Contains(output, "X-Original-From: sender@example.com") {
		t.Errorf("Expected X-Original-From header to be present. Got:\n%s", output)
	}

	// Check if body was mangled
	if !strings.Contains(output, "To: this should not be changed.") {
		t.Errorf("Body content was mangled. Got:\n%s", output)
	}
}

func TestProcess_MultiLineTo(t *testing.T) {
	input := `From: sender@example.com
To: first@mlctrez.com,
 second@mlctrez.com
Subject: Test Email

Body`
	from := "forwarder@mlctrez.com"
	to := "destination@gmail.com"

	reader := io.NopCloser(strings.NewReader(input))
	rawMessage := Process(reader, from, to)
	output := string(rawMessage.Data)

	if !strings.Contains(output, "To: destination@gmail.com\r\n") {
		t.Errorf("Expected To header to be updated. Got:\n%s", output)
	}

	if !strings.Contains(output, "X-Original-To: first@mlctrez.com,") {
		t.Errorf("Expected first part of original To. Got:\n%s", output)
	}
	if !strings.Contains(output, "X-Original-To-Cont:  second@mlctrez.com") {
		t.Errorf("Expected second part of original To. Got:\n%s", output)
	}
}
