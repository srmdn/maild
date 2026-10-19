package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/migrate"
	"github.com/srmdn/maild/internal/store/postgres"
)

func TestSignupHTTPAfterFreshDatabaseBootstrap(t *testing.T) {
	dsn := os.Getenv("MAILD_HTTP_TEST_DSN")
	redisAddr := os.Getenv("MAILD_HTTP_TEST_REDIS_ADDR")
	if dsn == "" || redisAddr == "" {
		t.Skip("dedicated MAILD_HTTP_TEST_DSN and MAILD_HTTP_TEST_REDIS_ADDR required")
	}

	ctx := context.Background()
	store, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	store.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { _ = store.Close() })

	schema := fmt.Sprintf("signup_flow_%d", time.Now().UnixNano())
	if _, err := store.DB().ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.DB().ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	if err := migrate.Up(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	sessions := NewSessionStore(redisAddr, 0)
	t.Cleanup(func() { _ = sessions.Close() })
	handler := NewAuthHandler(store, sessions, "development", true, "/login", nil, nil)
	signup := SameOrigin(handler.Signup)

	request := func(email string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]string{"email": email, "password": "a-long-test-password"})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "http://maild.test/api/v1/auth/signup", bytes.NewReader(body))
		req.Header.Set("Origin", "http://maild.test")
		response := httptest.NewRecorder()
		signup(response, req)
		return response
	}

	response := request("first@example.test")
	if response.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusCreated)
	}
	var created authResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.User.ID == 0 || created.User.WorkspaceID != 2 || created.User.Role != "admin" {
		t.Fatalf("signup result = %+v; want first user in workspace 2 as admin", created.User)
	}
	if cookie := response.Result().Cookies(); len(cookie) != 1 || cookie[0].Name != "maild_session" || cookie[0].Value == "" {
		t.Fatalf("signup cookies = %+v; want a session cookie", cookie)
	} else {
		_ = sessions.Delete(ctx, cookie[0].Value)
	}

	if _, err := store.DB().ExecContext(ctx, `ALTER TABLE workspaces ADD CONSTRAINT test_reject_signup_workspace CHECK (name <> 'rollback''s workspace')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(context.Background(), `ALTER TABLE workspaces DROP CONSTRAINT IF EXISTS test_reject_signup_workspace`)
	})
	failed := request("rollback@example.test")
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed signup status = %d, body = %s; want %d", failed.Code, failed.Body.String(), http.StatusInternalServerError)
	}
	var userCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM users WHERE email = 'rollback@example.test'`).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 0 {
		t.Fatalf("failed signup left %d user rows, want 0", userCount)
	}
}
