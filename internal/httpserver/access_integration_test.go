package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/api"
	"github.com/srmdn/maild/internal/auth"
	"github.com/srmdn/maild/internal/config"
	"github.com/srmdn/maild/internal/crypto"
	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/domaincheck"
	"github.com/srmdn/maild/internal/migrate"
	"github.com/srmdn/maild/internal/runtime"
	"github.com/srmdn/maild/internal/service"
	"github.com/srmdn/maild/internal/smtpclient"
	"github.com/srmdn/maild/internal/store/postgres"
	"github.com/srmdn/maild/internal/web"
)

type accessQueue struct{}

func (accessQueue) Enqueue(context.Context, int64) error                        { return nil }
func (accessQueue) Dequeue(context.Context, time.Duration) (int64, bool, error) { return 0, false, nil }

type accessLimiter struct{}

func (accessLimiter) Allow(context.Context, int64, string) (bool, string, error) {
	return true, "", nil
}

type accessFixture struct {
	store                 *postgres.Store
	handler               http.Handler
	users                 [2]domain.User
	workspaces            [2]int64
	cookies               [2]*http.Cookie
	adminKey, operatorKey string
}

// Use a database separate from MAILD_TEST_DSN: store tests truncate their fixture.
func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	dsn, redisAddr := os.Getenv("MAILD_HTTP_TEST_DSN"), os.Getenv("MAILD_HTTP_TEST_REDIS_ADDR")
	if dsn == "" || redisAddr == "" {
		t.Skip("dedicated MAILD_HTTP_TEST_DSN and MAILD_HTTP_TEST_REDIS_ADDR required")
	}
	ctx := context.Background()
	store, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := migrate.Up(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewSessionStore(redisAddr, 0)
	t.Cleanup(func() { sessions.Close() })
	f := &accessFixture{store: store, adminKey: "synthetic-instance-admin", operatorKey: "synthetic-instance-operator"}
	for i := range f.users {
		u, err := store.CreateUser(ctx, fmt.Sprintf("access-%d-%d@example.test", time.Now().UnixNano(), i), "unused-test-hash")
		if err != nil {
			t.Fatal(err)
		}
		f.users[i] = u
		ws, err := store.CreateWorkspaceForUser(ctx, u.ID, fmt.Sprintf("workspace-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		f.workspaces[i] = ws
		id, err := sessions.Create(ctx, u.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		f.cookies[i] = &http.Cookie{Name: "maild_session", Value: id}
		t.Cleanup(func() {
			_ = sessions.Delete(ctx, id)
			_, _ = store.DB().ExecContext(ctx, "DELETE FROM users WHERE id=$1", u.ID)
			_, _ = store.DB().ExecContext(ctx, "DELETE FROM workspaces WHERE id=$1", ws)
		})
	}
	cfg := config.Config{AppEnv: "development", APIKeyHeader: "X-Test-Key", AdminAPIKey: f.adminKey, OperatorAPIKey: f.operatorKey, LoginPath: "/login", AllowSignup: false, SMTPHost: "localhost", SMTPPort: 1025, SMTPFrom: "test@example.test"}
	authHandler := auth.NewAuthHandler(store, sessions, cfg.AppEnv, false, cfg.LoginPath, nil, nil)
	sealer, err := crypto.NewSealerFromBase64("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewMessageService(store, accessQueue{}, smtpclient.New(cfg), sealer, accessLimiter{}, nil, 3, 1, 400, 200, false, 0, 0, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := api.NewHandler(svc, service.NewDomainService(store, domaincheck.New()), cfg.AppEnv, cfg.APIKeyHeader, cfg.AdminAPIKey, cfg.OperatorAPIKey, false, "", "", "", time.Minute, logger, authHandler)
	f.handler = New(cfg, logger, runtime.NewDependencyState(), h, authHandler, web.FS).http.Handler
	return f
}

func (f *accessFixture) request(method, path string, body any, cookie *http.Cookie, key string) *httptest.ResponseRecorder {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "http://maild.test"+path, bytes.NewReader(data))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if key != "" {
		r.Header.Set("X-Test-Key", key)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func requireStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
	}
}

func TestHTTPWorkspaceBoundaries(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	_, err := f.store.CreateAPIKey(ctx, f.users[0].ID, f.workspaces[0], "scope", "scoped-hash", "mk_scope")
	if err != nil {
		t.Fatal(err)
	}
	raw := "synthetic-scoped-key"
	_, err = f.store.CreateAPIKey(ctx, f.users[0].ID, f.workspaces[0], "actual", crypto.HashAPIKey(raw), crypto.APIKeyPrefix(raw))
	if err != nil {
		t.Fatal(err)
	}
	message, err := f.store.CreateMessage(ctx, domain.Message{WorkspaceID: f.workspaces[1], FromEmail: "a@example.test", ToEmail: "b@example.test", Subject: "private marker", BodyText: "private body", Status: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.InsertWebhookEvent(ctx, domain.WebhookEvent{WorkspaceID: f.workspaces[1], EventType: "bounce", Email: "b@example.test", Status: "dead_letter"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"session", "key"} {
		t.Run(mode, func(t *testing.T) {
			var cookie *http.Cookie
			key := ""
			if mode == "session" {
				cookie = f.cookies[0]
			} else {
				key = raw
			}
			for _, path := range []string{"/v1/messages/logs", "/v1/smtp-accounts/list", "/v1/webhooks/logs", "/v1/workspaces/policy", "/v1/analytics/summary", "/v1/analytics/export.csv", "/v1/billing/metering", "/v1/ops/onboarding-checklist"} {
				t.Run(path, func(t *testing.T) {
					requireStatus(t, f.request("GET", fmt.Sprintf("%s?workspace_id=%d", path, f.workspaces[1]), nil, cookie, key), 403)
					w := f.request("GET", path, nil, cookie, key)
					requireStatus(t, w, 200)
					if strings.Contains(w.Body.String(), "private marker") {
						t.Fatal("foreign data leaked")
					}
				})
			}
			for _, path := range []string{"/v1/messages", "/v1/suppressions", "/v1/unsubscribes", "/v1/smtp-accounts", "/v1/smtp-accounts/activate", "/v1/smtp-accounts/validate", "/v1/domains/readiness", "/v1/messages/retry", "/v1/webhooks/replay", "/v1/workspaces/policy"} {
				t.Run("POST "+path, func(t *testing.T) {
					requireStatus(t, f.request("POST", path, map[string]any{"workspace_id": f.workspaces[1]}, cookie, key), 403)
				})
			}
			requireStatus(t, f.request("GET", fmt.Sprintf("/v1/messages/timeline?message_id=%d", message.ID), nil, cookie, key), 404)
			requireStatus(t, f.request("GET", "/v1/messages/timeline?message_id=999999999999", nil, cookie, key), 404)
			requireStatus(t, f.request("GET", fmt.Sprintf("/v1/incidents/bundle?message_id=%d", message.ID), nil, cookie, key), 404)
			requireStatus(t, f.request("GET", "/v1/incidents/bundle?message_id=999999999999", nil, cookie, key), 404)
			requireStatus(t, f.request("GET", "/v1/messages/logs?workspace_id=-1", nil, cookie, key), 400)
			requireStatus(t, f.request("GET", "/v1/messages/logs?workspace_id=1&workspace_id=2", nil, cookie, key), 400)
		})
	}
	for _, path := range []string{"/ui", "/ui/logs", "/ui/onboarding", "/ui/incidents", "/ui/policy"} {
		requireStatus(t, f.request("GET", fmt.Sprintf("%s?workspace_id=%d", path, f.workspaces[1]), nil, f.cookies[0], ""), 403)
		requireStatus(t, f.request("GET", path, nil, f.cookies[0], ""), 200)
	}
	for _, key := range []string{f.adminKey, f.operatorKey} {
		requireStatus(t, f.request("GET", fmt.Sprintf("/v1/messages/timeline?message_id=%d", message.ID), nil, nil, key), 200)
	}
	requireStatus(t, f.request("GET", "/v1/messages/logs", nil, f.cookies[0], "invalid-key"), 401)
	w := f.request("GET", "/dashboard", nil, f.cookies[0], "")
	requireStatus(t, w, 200)
	for _, key := range []string{f.adminKey, f.operatorKey, raw} {
		if strings.Contains(w.Body.String(), key) {
			t.Fatal("credential embedded in dashboard")
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authenticated dashboard may be cached")
	}
	r := httptest.NewRequest("GET", "http://maild.test/v1/messages/logs", nil)
	r.AddCookie(f.cookies[0])
	r.Header.Set("X-Test-Key", "")
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	requireStatus(t, w, 401)
	for _, path := range []string{"/login", "/logout", "/api/v1/user/keys/create", "/v1/workspaces/policy"} {
		r := httptest.NewRequest("POST", "http://maild.test"+path, strings.NewReader(`{}`))
		r.AddCookie(f.cookies[0])
		r.Header.Set("Origin", "https://other.test")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		requireStatus(t, w, 403)
	}
}

func TestHTTPPersonalKeyLifecycleAndRoles(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	w := f.request("POST", "/api/v1/user/keys/create", map[string]string{"name": "<b>literal key name</b>"}, f.cookies[0], "")
	requireStatus(t, w, 201)
	var key domain.CreateAPIKeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
		t.Fatal(err)
	}
	if key.KeyPrefix != crypto.APIKeyPrefix(key.Key) {
		t.Fatal("bad prefix")
	}
	w = f.request("POST", "/v1/messages", map[string]string{"from_email": "a@example.test", "to_email": "b@example.test", "subject": "<b>literal subject</b>"}, nil, key.Key)
	requireStatus(t, w, 201)
	var msg domain.Message
	if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.WorkspaceID != f.workspaces[0] {
		t.Fatal("omitted workspace did not use key scope")
	}
	requireStatus(t, f.request("GET", fmt.Sprintf("/v1/messages/timeline?message_id=%d", msg.ID), nil, nil, key.Key), 200)
	w = f.request("GET", "/api/v1/user/keys", nil, f.cookies[0], "")
	requireStatus(t, w, 200)
	var keys []domain.UserAPIKey
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].KeyPrefix != key.KeyPrefix || keys[0].LastUsedAt == nil || strings.Contains(w.Body.String(), key.Key) {
		t.Fatal("key list leaked key or lost metadata")
	}
	requireStatus(t, f.request("POST", "/v1/workspaces/policy", map[string]any{
		"rate_limit_workspace_per_hour": 300, "rate_limit_domain_per_hour": 100,
	}, f.cookies[0], ""), 200)
	w = f.request("GET", "/v1/workspaces/policy", nil, nil, key.Key)
	requireStatus(t, w, 200)
	var policy domain.WorkspacePolicy
	if err := json.Unmarshal(w.Body.Bytes(), &policy); err != nil || policy.WorkspaceID != f.workspaces[0] || policy.RateLimitWorkspacePerHour != 300 {
		t.Fatalf("session write/key read policy mismatch: %+v err=%v", policy, err)
	}
	requireStatus(t, f.request("DELETE", "/api/v1/user/keys", map[string]int64{"id": key.ID}, f.cookies[1], ""), 404)
	for _, role := range []string{"operator", "viewer"} {
		if _, err := f.store.DB().ExecContext(ctx, "UPDATE user_workspaces SET role=$1 WHERE user_id=$2 AND workspace_id=$3", role, f.users[0].ID, f.workspaces[0]); err != nil {
			t.Fatal(err)
		}
		for _, useSession := range []bool{false, true} {
			var cookie *http.Cookie
			credential := key.Key
			if useSession {
				cookie = f.cookies[0]
				credential = ""
			}
			requireStatus(t, f.request("GET", "/v1/messages/logs", nil, cookie, credential), 200)
			requireStatus(t, f.request("POST", "/v1/workspaces/policy", map[string]any{}, cookie, credential), 403)
			requireStatus(t, f.request("POST", "/v1/suppressions", map[string]string{"email": "x@example.test"}, cookie, credential), 403)
			want := 201
			if role == "viewer" {
				want = 403
			}
			requireStatus(t, f.request("POST", "/v1/messages", map[string]string{"from_email": "a@example.test", "to_email": "b@example.test", "subject": "role check"}, cookie, credential), want)
		}
	}
	requireStatus(t, f.request("POST", "/api/v1/user/keys/create", map[string]string{"name": "viewer"}, f.cookies[0], ""), 403)
	r := httptest.NewRequest("DELETE", "http://maild.test/api/v1/user/keys", strings.NewReader(fmt.Sprintf(`{"id":%d}`, key.ID)))
	r.AddCookie(f.cookies[0])
	r.Header.Set("Origin", "https://other.test")
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	requireStatus(t, w, 403)
	requireStatus(t, f.request("DELETE", "/api/v1/user/keys", map[string]int64{"id": key.ID}, f.cookies[0], ""), 200)
	requireStatus(t, f.request("GET", "/v1/messages/logs", nil, nil, key.Key), 401)
	if _, err := f.store.DB().ExecContext(ctx, "DELETE FROM user_workspaces WHERE user_id=$1", f.users[0].ID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("GET", "/v1/messages/logs", nil, f.cookies[0], ""), 401)
}
