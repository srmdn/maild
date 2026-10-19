package smtpclient

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestValidateBoundsStalledGreeting(t *testing.T) {
	listener := listen(t)
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			time.Sleep(time.Second)
		}
	}()

	client := NewTestClient()
	creds := testCredentials(listener.Addr(), TLSModeNone)
	started := time.Now()
	err := client.Validate(context.Background(), creds, 100*time.Millisecond)
	if err == nil {
		t.Fatal("Validate succeeded against a server that never sent a greeting")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Validate exceeded its deadline: %s", elapsed)
	}
}

func TestValidateStopsWhenContextIsCanceled(t *testing.T) {
	listener := listen(t)
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			close(accepted)
			time.Sleep(time.Second)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- NewTestClient().Validate(ctx, testCredentials(listener.Addr(), TLSModeNone), 5*time.Second)
	}()
	select {
	case <-accepted:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("SMTP client did not connect")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Validate succeeded after its context was canceled")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Validate did not stop promptly after context cancellation")
	}
}

func TestSendBoundsStalledData(t *testing.T) {
	listener := listen(t)
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("220 test ESMTP\r\n"))
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			switch strings.ToUpper(strings.TrimSpace(line)) {
			case "EHLO LOCALHOST":
				_, _ = conn.Write([]byte("250 test\r\n"))
			case "MAIL FROM:<sender@example.com>":
				_, _ = conn.Write([]byte("250 ok\r\n"))
			case "RCPT TO:<recipient@example.com>":
				_, _ = conn.Write([]byte("250 ok\r\n"))
			case "DATA":
				_, _ = conn.Write([]byte("354 send data\r\n"))
				// Deliberately never read or acknowledge the message body.
				time.Sleep(time.Second)
				return
			case "QUIT":
				_, _ = conn.Write([]byte("221 bye\r\n"))
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	err := NewTestClient().Send(ctx, testCredentials(listener.Addr(), TLSModeNone), "recipient@example.com", "test", "body")
	if err == nil {
		t.Fatal("Send succeeded against a server stalled during DATA")
	}
}

func TestValidateRequiresStartTLS(t *testing.T) {
	listener := listen(t)
	defer listener.Close()
	serveSMTP(t, listener, func(conn net.Conn) {
		_, _ = conn.Write([]byte("220 test ESMTP\r\n"))
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadString('\n'); err == nil {
			_, _ = conn.Write([]byte("250 test\r\n")) // no STARTTLS extension
		}
	})

	err := NewTestClient().Validate(context.Background(), testCredentials(listener.Addr(), TLSModeStartTLS), time.Second)
	if err != ErrTLSRequired {
		t.Fatalf("Validate error = %v, want %v", err, ErrTLSRequired)
	}
}

func TestValidateAuthenticatesOnlyAfterSTARTTLS(t *testing.T) {
	cert := testCertificate(t)
	listener := listen(t)
	defer listener.Close()
	commands := make(chan string, 8)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("220 test ESMTP\r\n"))
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		commands <- strings.TrimSpace(line)
		_, _ = conn.Write([]byte("250-test\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n"))
		line, err = reader.ReadString('\n')
		if err != nil {
			return
		}
		commands <- strings.TrimSpace(line)
		_, _ = conn.Write([]byte("220 begin TLS\r\n"))
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if tlsConn.Handshake() != nil {
			return
		}
		reader = bufio.NewReader(tlsConn)
		for {
			line, err = reader.ReadString('\n')
			if err != nil {
				return
			}
			command := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToUpper(command), "AUTH PLAIN ") {
				commands <- "AUTH PLAIN <redacted>"
				_, _ = tlsConn.Write([]byte("235 authenticated\r\n"))
				continue
			}
			commands <- command
			switch {
			case strings.HasPrefix(strings.ToUpper(command), "EHLO "):
				_, _ = tlsConn.Write([]byte("250-test\r\n250 AUTH PLAIN\r\n"))
			case strings.EqualFold(command, "AUTH PLAIN"):
				_, _ = tlsConn.Write([]byte("334 continue\r\n"))
				if _, err = reader.ReadString('\n'); err != nil {
					return
				}
				_, _ = tlsConn.Write([]byte("235 authenticated\r\n"))
			case strings.EqualFold(command, "NOOP"):
				_, _ = tlsConn.Write([]byte("250 ok\r\n"))
			case strings.EqualFold(command, "QUIT"):
				_, _ = tlsConn.Write([]byte("221 bye\r\n"))
				return
			}
		}
	}()

	creds := testCredentials(listener.Addr(), TLSModeStartTLS)
	creds.Username, creds.Password = "user", "secret"
	creds.Host = "localhost"
	client := clientWithTestCert(t, cert)
	if err := client.Validate(context.Background(), creds, 2*time.Second); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	got := []string{<-commands, <-commands, <-commands, <-commands, <-commands}
	if !strings.EqualFold(got[1], "STARTTLS") {
		t.Fatalf("second command = %q, want STARTTLS; commands: %v", got[1], got)
	}
	if !strings.HasPrefix(got[3], "AUTH PLAIN") {
		t.Fatalf("AUTH was not sent after STARTTLS; observed command order: %q, %q, %q, %q, %q", got[0], got[1], got[2], got[3], got[4])
	}
}

func TestImplicitTLSValidation(t *testing.T) {
	cert := testCertificate(t)
	listener := tls.NewListener(listen(t), &tls.Config{Certificates: []tls.Certificate{cert}})
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("220 test ESMTP\r\n"))
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(strings.ToUpper(line), "EHLO "):
				_, _ = conn.Write([]byte("250 test\r\n"))
			case strings.EqualFold(strings.TrimSpace(line), "NOOP"):
				_, _ = conn.Write([]byte("250 ok\r\n"))
			case strings.EqualFold(strings.TrimSpace(line), "QUIT"):
				_, _ = conn.Write([]byte("221 bye\r\n"))
				return
			}
		}
	}()

	creds := testCredentials(listener.Addr(), TLSModeImplicit)
	creds.Host = "localhost"
	if err := clientWithTestCert(t, cert).Validate(context.Background(), creds, 2*time.Second); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestPlainModeRejectsAuthentication(t *testing.T) {
	creds := Credentials{Host: "localhost", Port: 25, Username: "user", TLSMode: TLSModeNone}
	_, _, err := NewTestClient().connect(context.Background(), creds)
	if err == nil || !strings.Contains(err.Error(), "requires TLS") {
		t.Fatalf("connect error = %v, want authentication TLS requirement", err)
	}
}

func NewTestClient() *Client { return &Client{tlsConfig: tlsConfig} }

func testCredentials(addr net.Addr, mode string) Credentials {
	host, portText, _ := net.SplitHostPort(addr.String())
	port, _ := strconv.Atoi(portText)
	return Credentials{Host: host, Port: port, From: "sender@example.com", TLSMode: mode}
}

func clientWithTestCert(t *testing.T, cert tls.Certificate) *Client {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return &Client{tlsConfig: func(host string) *tls.Config {
		return &tls.Config{ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS12}
	}}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener
}

func serveSMTP(t *testing.T, listener net.Listener, serve func(net.Conn)) {
	t.Helper()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			serve(conn)
		}
	}()
}

func testCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		DNSNames:              []string{"localhost"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
