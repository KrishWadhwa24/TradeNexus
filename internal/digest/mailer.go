package digest

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
)

// SMTPConfig is the digest sender's mail account — plain SMTP (e.g. a
// Gmail/Workspace app password), no third-party email-provider dependency.
type SMTPConfig struct {
	Host, Port, User, Password, From string
}

func (c SMTPConfig) addr() string { return c.Host + ":" + c.Port }

// sendMail hand-builds a multipart/mixed message (HTML body + a base64 PDF
// attachment) and sends it over stdlib net/smtp — no email-library
// dependency needed for something this simple.
func sendMail(cfg SMTPConfig, to, subject, htmlBody string, attachment []byte, attachmentName string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fmt.Fprintf(&buf, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", cfg.From, to, subject)
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", w.Boundary())

	bodyPart, err := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/html; charset=UTF-8"}})
	if err != nil {
		return err
	}
	if _, err := bodyPart.Write([]byte(htmlBody)); err != nil {
		return err
	}

	attPart, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"application/pdf"},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {fmt.Sprintf(`attachment; filename=%q`, attachmentName)},
	})
	if err != nil {
		return err
	}
	enc := base64.NewEncoder(base64.StdEncoding, attPart)
	if _, err := enc.Write(attachment); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	if err := w.Close(); err != nil {
		return err
	}

	auth := smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Host)
	return smtp.SendMail(cfg.addr(), auth, cfg.From, []string{to}, buf.Bytes())
}
