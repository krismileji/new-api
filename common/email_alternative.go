package common

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// SendEmailAlternativeWithTimeout shares the configured SMTP transport with
// existing notifications. A stable message ID helps recipients deduplicate retries.
func SendEmailAlternativeWithTimeout(subject, receiver, plain, html, messageID string, timeout time.Duration) error {
	address, err := mail.ParseAddress(receiver)
	if err != nil || address.Address != receiver || strings.ContainsAny(receiver+messageID+SystemName+SMTPFrom+SMTPAccount, "\r\n") {
		return fmt.Errorf("邮件地址或邮件头无效")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range []struct{ kind, content string }{{"text/plain", plain}, {"text/html", html}} {
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", part.kind+"; charset=UTF-8")
		header.Set("Content-Transfer-Encoding", "8bit")
		output, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		if _, err := output.Write([]byte(part.content)); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return sendEmailWithContentType(subject, receiver, body.String(), "multipart/alternative; boundary="+writer.Boundary(), messageID, timeout)
}
