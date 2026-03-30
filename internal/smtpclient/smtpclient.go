package smtpclient

import (
	"fmt"
	"net/smtp"
	"strings"

	"github.com/srmdn/maild/internal/config"
)

type Client struct {
	defaultCreds Credentials
}

type Credentials struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
}

func New(cfg config.Config) *Client {
	return &Client{
		defaultCreds: Credentials{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
		},
	}
}

func (c *Client) DefaultCredentials() Credentials {
	return c.defaultCreds
}

func ProviderName(creds Credentials) string {
	return fmt.Sprintf("%s:%d", creds.Host, creds.Port)
}

func (c *Client) Send(creds Credentials, toEmail, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", creds.Host, creds.Port)
	msg := buildMessage(creds.From, toEmail, subject, body)

	var auth smtp.Auth
	if creds.Username != "" || creds.Password != "" {
		auth = smtp.PlainAuth("", creds.Username, creds.Password, creds.Host)
	}

	return smtp.SendMail(addr, auth, creds.From, []string{toEmail}, []byte(msg))
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
