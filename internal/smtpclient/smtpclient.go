package smtpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/srmdn/maild/internal/config"
)

const (
	TLSModeStartTLS = "starttls"
	TLSModeImplicit = "implicit_tls"
	TLSModeNone     = "none"
	DefaultTLSMode  = TLSModeStartTLS
	sendTimeout     = 30 * time.Second
)

var ErrTLSRequired = errors.New("smtp TLS is required but unavailable")

type Client struct {
	defaultCreds Credentials
	tlsConfig    func(string) *tls.Config
}

type Credentials struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	TLSMode  string `json:"tls_mode,omitempty"`
}

func New(cfg config.Config) *Client {
	mode := cfg.SMTPTLSMode
	if mode == "" {
		mode = DefaultTLSMode
	}
	return &Client{
		defaultCreds: Credentials{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			TLSMode:  mode,
		},
		tlsConfig: tlsConfig,
	}
}

func (c *Client) DefaultCredentials() Credentials {
	return c.defaultCreds
}

func ProviderName(creds Credentials) string {
	return address(creds.Host, creds.Port)
}

func NormalizeTLSMode(mode string) (string, error) {
	if mode == "" {
		return DefaultTLSMode, nil
	}
	switch mode {
	case TLSModeStartTLS, TLSModeImplicit, TLSModeNone:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported SMTP TLS mode %q", mode)
	}
}

func (c *Client) Send(ctx context.Context, creds Credentials, toEmail, subject, body string) error {
	ctx, cancel := boundedContext(ctx, sendTimeout)
	defer cancel()

	client, conn, err := c.connect(ctx, creds)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer client.Close()

	if err := authenticate(client, creds); err != nil {
		return err
	}
	if err := client.Mail(creds.From); err != nil {
		return err
	}
	if err := client.Rcpt(toEmail); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, buildMessage(creds.From, toEmail, subject, body)); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (c *Client) Validate(ctx context.Context, creds Credentials, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = sendTimeout
	}
	ctx, cancel := boundedContext(ctx, timeout)
	defer cancel()

	client, conn, err := c.connect(ctx, creds)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer client.Close()

	if err := authenticate(client, creds); err != nil {
		return err
	}
	return client.Noop()
}

func (c *Client) connect(ctx context.Context, creds Credentials) (*smtp.Client, net.Conn, error) {
	mode, err := NormalizeTLSMode(creds.TLSMode)
	if err != nil {
		return nil, nil, err
	}
	if mode == TLSModeNone && (creds.Username != "" || creds.Password != "") {
		return nil, nil, errors.New("SMTP authentication requires TLS")
	}

	addr := address(creds.Host, creds.Port)
	dialer := &net.Dialer{}
	configForHost := c.tlsConfig
	if configForHost == nil {
		configForHost = tlsConfig
	}
	var conn net.Conn
	if mode == TLSModeImplicit {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: configForHost(creds.Host)}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, nil, err
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stopClose := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopClose:
		}
	}()
	stop := func() { close(stopClose) }

	client, err := smtp.NewClient(conn, creds.Host)
	if err != nil {
		stop()
		_ = conn.Close()
		return nil, nil, err
	}
	if mode == TLSModeStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			stop()
			_ = client.Close()
			return nil, nil, ErrTLSRequired
		}
		if err := client.StartTLS(configForHost(creds.Host)); err != nil {
			stop()
			_ = client.Close()
			return nil, nil, err
		}
	}
	return client, &managedConn{Conn: conn, stop: stop}, nil
}

type managedConn struct {
	net.Conn
	stop func()
}

func (c *managedConn) Close() error {
	c.stop()
	return c.Conn.Close()
}

func tlsConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

func authenticate(client *smtp.Client, creds Credentials) error {
	if creds.Username == "" && creds.Password == "" {
		return nil
	}
	if _, ok := client.TLSConnectionState(); !ok {
		return errors.New("SMTP authentication requires TLS")
	}
	return client.Auth(smtp.PlainAuth("", creds.Username, creds.Password, creds.Host))
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= timeout {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func address(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
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
