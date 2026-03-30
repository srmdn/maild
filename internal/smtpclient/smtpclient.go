package smtpclient

import (
	"fmt"
	"net/smtp"
	"strings"

	"github.com/srmdn/maild/internal/config"
)

type Client struct {
	host     string
	port     int
	username string
	password string
	from     string
}

func New(cfg config.Config) *Client {
	return &Client{
		host:     cfg.SMTPHost,
		port:     cfg.SMTPPort,
		username: cfg.SMTPUsername,
		password: cfg.SMTPPassword,
		from:     cfg.SMTPFrom,
	}
}

func (c *Client) ProviderName() string {
	return fmt.Sprintf("%s:%d", c.host, c.port)
}

func (c *Client) Send(toEmail, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	msg := buildMessage(c.from, toEmail, subject, body)

	var auth smtp.Auth
	if c.username != "" || c.password != "" {
		auth = smtp.PlainAuth("", c.username, c.password, c.host)
	}

	return smtp.SendMail(addr, auth, c.from, []string{toEmail}, []byte(msg))
}

func buildMessage(from, to, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}
