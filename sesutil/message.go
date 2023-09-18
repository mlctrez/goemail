package sesutil

import (
	"bufio"
	"bytes"
	"fmt"
	sesTypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
	"io"
	"strings"
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

	for scanner.Scan() {
		l := scanner.Text()
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			mp.additional = append(mp.additional, l)
		} else {
			if mp != nil {
				mp.write(buf, from, to)
			}
			mp = &part{first: l}
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
			case "to:":
				writeLine(fmt.Sprintf("To: %s", to))
			}
			return
		}
	}

	writeLine(m.first)
	for _, s := range m.additional {
		writeLine(s)
	}
}
