package worker

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/config"
	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/migrate"
	"github.com/srmdn/maild/internal/queue"
	"github.com/srmdn/maild/internal/service"
	"github.com/srmdn/maild/internal/smtpclient"
	"github.com/srmdn/maild/internal/store/postgres"
)

const workerKillHelperEnv = "MAILD_A04_WORKER_KILL_HELPER"

func TestA04WorkerKillAfterSMTPBodyRecordsUnknownOnRecovery(t *testing.T) {
	dsn := os.Getenv("MAILD_TEST_DSN")
	redisAddr := os.Getenv("MAILD_TEST_REDIS_ADDR")
	if dsn == "" || redisAddr == "" {
		t.Skip("MAILD_TEST_DSN and MAILD_TEST_REDIS_ADDR must point to disposable local test services")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := migrate.Up(ctx, store.DB()); err != nil {
		t.Fatalf("migrate test store: %v", err)
	}
	// MAILD_TEST_DSN must name a dedicated disposable database; this test truncates its fixtures.
	if _, err := store.DB().ExecContext(ctx, `TRUNCATE TABLE message_attempts, messages, suppressions, unsubscribes, smtp_accounts, webhook_events, workspace_policies, metering_events, domains, user_api_keys, user_workspaces, users, workspaces RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("clear disposable test store: %v", err)
	}
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for fake SMTP relay: %v", err)
	}
	defer listener.Close()
	bodyReceived := make(chan struct{}, 1)
	smtpCommands := make(chan string, 32)
	go serveSMTPUntilBodyReceived(listener, bodyReceived, smtpCommands)

	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("parse fake SMTP port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse fake SMTP port: %v", err)
	}
	message, err := store.CreateMessage(ctx, domain.Message{
		WorkspaceID: 1,
		FromEmail:   "sender@example.test",
		ToEmail:     "recipient@example.test",
		Subject:     "process-kill drill",
		BodyText:    "relay received body before worker was killed",
		Status:      "queued",
	})
	if err != nil {
		t.Fatal(err)
	}

	q, err := queue.NewRedis(ctx, redisAddr, 0)
	if err != nil {
		t.Fatalf("connect disposable Redis: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	t.Cleanup(func() {
		_ = q.Client().Del(context.Background(), "maild:queue:messages", "maild:queue:retry", "maild:queue:last-source").Err()
	})
	if err := q.Client().Del(ctx, "maild:queue:messages", "maild:queue:retry", "maild:queue:last-source").Err(); err != nil {
		t.Fatalf("clear disposable Redis queue: %v", err)
	}
	if err := q.Enqueue(ctx, message.ID); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestA04WorkerKillHelper$")
	cmd.Env = append(os.Environ(),
		workerKillHelperEnv+"=1",
		"MAILD_TEST_DSN="+dsn,
		"MAILD_TEST_REDIS_ADDR="+redisAddr,
		"MAILD_A04_MESSAGE_ID="+strconv.FormatInt(message.ID, 10),
		"MAILD_A04_SMTP_PORT="+strconv.Itoa(port),
	)
	var childOutput bytes.Buffer
	cmd.Stdout = &childOutput
	cmd.Stderr = &childOutput
	if err := cmd.Start(); err != nil {
		t.Fatalf("start worker subprocess: %v", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case <-bodyReceived:
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("kill worker after relay received body: %v", err)
		}
		if err := <-wait; err == nil {
			t.Fatal("worker subprocess exited cleanly; expected process kill")
		}
	case err := <-wait:
		t.Fatalf("worker exited before relay received message body: %v; child output: %s", err, childOutput.String())
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-wait
		var observed []string
		for {
			select {
			case command := <-smtpCommands:
				observed = append(observed, command)
			default:
				t.Fatalf("worker did not reach SMTP DATA before timeout; commands: %q; child output: %s", observed, childOutput.String())
			}
		}
	}

	// Reopen the durable stores as a fresh application process would after restart.
	if err := store.Close(); err != nil {
		t.Fatalf("close pre-crash store handle: %v", err)
	}
	store, err = postgres.New(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen test store after worker crash: %v", err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("close pre-crash Redis handle: %v", err)
	}
	q, err = queue.NewRedis(ctx, redisAddr, 0)
	if err != nil {
		t.Fatalf("reopen Redis after worker crash: %v", err)
	}

	// Fast-forward the stale lease instead of waiting the production 15 minutes.
	if _, err := store.DB().ExecContext(ctx, `UPDATE messages SET updated_at = $2 WHERE id = $1`, message.ID, time.Now().Add(-16*time.Minute)); err != nil {
		t.Fatal(err)
	}
	serviceAfterRestart := service.NewMessageService(store, q, nil, nil, nil, nil, 1, 1, 0, 0, false, 0, 0, 0)
	recovery, err := serviceAfterRestart.RecoverQueue(ctx, 30*time.Second, 15*time.Minute, 10)
	if err != nil {
		t.Fatalf("recover after worker restart: %v", err)
	}
	if recovery.Requeued != 0 || recovery.MarkedUnknown != 1 {
		t.Fatalf("recovery result = %+v, want no resend and one unknown outcome", recovery)
	}
	got, err := store.GetMessage(ctx, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "unknown" {
		t.Fatalf("message status after restart = %q, want unknown", got.Status)
	}
	if queued, err := q.Client().LLen(ctx, "maild:queue:messages").Result(); err != nil {
		t.Fatal(err)
	} else if queued != 0 {
		t.Fatalf("ready queue contains %d messages after ambiguous SMTP recovery, want 0", queued)
	}
}

func TestA04WorkerKillHelper(t *testing.T) {
	if os.Getenv(workerKillHelperEnv) != "1" {
		return
	}
	ctx := context.Background()
	store, err := postgres.New(ctx, os.Getenv("MAILD_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	q, err := queue.NewRedis(ctx, os.Getenv("MAILD_TEST_REDIS_ADDR"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	messageID, err := strconv.ParseInt(os.Getenv("MAILD_A04_MESSAGE_ID"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := q.Dequeue(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != messageID {
		t.Fatalf("dequeued (%d, %t), want message %d", got, ok, messageID)
	}
	port, err := strconv.Atoi(os.Getenv("MAILD_A04_SMTP_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	sender := smtpclient.New(config.Config{
		SMTPHost:    "127.0.0.1",
		SMTPPort:    port,
		SMTPFrom:    "sender@example.test",
		SMTPTLSMode: smtpclient.TLSModeNone,
	})
	svc := service.NewMessageService(store, q, sender, nil, nil, nil, 1, 1, 0, 0, false, 0, 0, 0)
	if err := svc.ProcessOne(ctx, messageID); err != nil {
		t.Fatal(err)
	}
	// The parent kills this process while Send waits for the SMTP final response.
	t.Fatal("SMTP relay did not hold the worker open")
}

func serveSMTPUntilBodyReceived(listener net.Listener, bodyReceived chan<- struct{}, commands chan<- string) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = fmt.Fprint(conn, "220 test ESMTP\r\n")
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		commands <- command
		switch {
		case strings.HasPrefix(command, "EHLO ") || strings.HasPrefix(command, "HELO "):
			_, _ = fmt.Fprint(conn, "250 test\r\n")
		case strings.HasPrefix(command, "MAIL FROM:"):
			_, _ = fmt.Fprint(conn, "250 ok\r\n")
		case strings.HasPrefix(command, "RCPT TO:"):
			_, _ = fmt.Fprint(conn, "250 ok\r\n")
		case command == "DATA":
			_, _ = fmt.Fprint(conn, "354 send data\r\n")
			for {
				dataLine, readErr := reader.ReadString('\n')
				if readErr != nil {
					return
				}
				if dataLine == ".\r\n" {
					bodyReceived <- struct{}{}
					_, _ = reader.ReadString('\n') // Hold the connection until the worker is killed.
					return
				}
			}
		case command == "QUIT":
			return
		}
	}
}
