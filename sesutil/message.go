package sesutil

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"

	sesTypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
)

type part struct {
	first      string
	additional []string
}

func Process(reader io.ReadCloser, from, to string) *sesTypes.RawMessage {
	defer func() { _ = reader.Close() }()
	buf := &bytes.Buffer{}
	scanner := bufio.NewScanner(reader)
	var mp *part

	headerMode := true
	for scanner.Scan() {
		l := scanner.Text()
		if headerMode && l == "" {
			headerMode = false
			if mp != nil {
				mp.write(buf, from, to)
				mp = nil
			}
			buf.WriteString("\r\n")
			continue
		}

		if headerMode {
			if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
				if mp != nil {
					mp.additional = append(mp.additional, l)
				}
			} else {
				if mp != nil {
					mp.write(buf, from, to)
				}
				mp = &part{first: l}
			}
		} else {
			buf.WriteString(l + "\r\n")
		}
	}
	if mp != nil {
		mp.write(buf, from, to)
	}

	return &sesTypes.RawMessage{Data: buf.Bytes()}
}

var ignoreHeaders = []string{
	"dkim-", "from:", "return-path:", "to:", "sender:", "list-owner:",
}

func (m *part) write(b *bytes.Buffer, from, to string) {

	writeLine := func(in string) {
		b.WriteString(fmt.Sprintf("%s\r\n", in))
	}

	firstLower := strings.ToLower(m.first)
	for _, header := range ignoreHeaders {
		if strings.HasPrefix(firstLower, header) {
			switch header {
			case "from:":
				writeLine(fmt.Sprintf("From: %s", from))
				writeLine(fmt.Sprintf("X-Original-From: %s", strings.TrimSpace(m.first[len("from:"):])))
				for _, s := range m.additional {
					writeLine(fmt.Sprintf("X-Original-From-Cont: %s", s))
				}
			case "to:":
				writeLine(fmt.Sprintf("To: %s", to))
				writeLine(fmt.Sprintf("X-Original-To: %s", strings.TrimSpace(m.first[len("to:"):])))
				for _, s := range m.additional {
					writeLine(fmt.Sprintf("X-Original-To-Cont: %s", s))
				}
			}
			return
		}
	}

	writeLine(m.first)
	for _, s := range m.additional {
		writeLine(s)
	}
}
