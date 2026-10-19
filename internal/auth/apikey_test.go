package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/srmdn/maild/internal/crypto"
	"github.com/srmdn/maild/internal/domain"
)

type keyLookup struct {
	identity domain.APIKeyIdentity
	err      error
	hash     string
}

func (s *keyLookup) AuthenticateAPIKey(_ context.Context, hash string) (domain.APIKeyIdentity, error) {
	s.hash = hash
	return s.identity, s.err
}

func TestAPIKeyCredentialsAndScope(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		identity  domain.APIKeyIdentity
		err       error
		status    int
		global    bool
	}{
		{name: "missing", status: 401},
		{name: "global admin", key: "instance-admin", status: 204, global: true},
		{name: "global operator", key: "instance-operator", status: 204, global: true},
		{name: "personal", key: "personal", identity: domain.APIKeyIdentity{UserID: 7, WorkspaceID: 9, Role: "operator"}, status: 204},
		{name: "viewer", key: "personal", identity: domain.APIKeyIdentity{UserID: 7, WorkspaceID: 9, Role: "viewer"}, status: 204},
		{name: "revoked", key: "personal", err: sql.ErrNoRows, status: 401},
		{name: "store failure", key: "personal", err: errors.New("synthetic failure"), status: 500},
		{name: "unknown role", key: "personal", identity: domain.APIKeyIdentity{UserID: 7, WorkspaceID: 9, Role: "owner"}, status: 401},
		{name: "missing membership", key: "personal", identity: domain.APIKeyIdentity{UserID: 7, Role: "admin"}, status: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &keyLookup{identity: tc.identity, err: tc.err}
			handler := APIKeyMiddleware("X-Test-Key", "instance-admin", "instance-operator", store, func(w http.ResponseWriter, r *http.Request) {
				p, ok := PrincipalFromContext(r.Context())
				if !ok || p.Global != tc.global {
					t.Fatalf("principal=%+v ok=%v", p, ok)
				}
				if !p.Global && (p.UserID != 7 || p.WorkspaceID != 9) {
					t.Fatal("lost user scope")
				}
				w.WriteHeader(204)
			})
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("X-Test-Key", tc.key)
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
			if tc.key == "personal" && store.hash != crypto.HashAPIKey(tc.key) {
				t.Fatal("store did not receive only the hash")
			}
		})
	}
	if _, ok := resolveRole("", "", ""); ok {
		t.Fatal("empty keys must not authenticate")
	}
}

func TestWorkspaceAndRoleBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name            string
		principal       Principal
		requested, want int64
		err             error
	}{
		{"own default", Principal{UserID: 7, WorkspaceID: 9, Role: RoleAdmin}, 0, 9, nil},
		{"own explicit", Principal{UserID: 7, WorkspaceID: 9, Role: RoleOperator}, 9, 9, nil},
		{"other workspace", Principal{UserID: 7, WorkspaceID: 9, Role: RoleAdmin}, 10, 0, ErrForbidden},
		{"negative", Principal{UserID: 7, WorkspaceID: 9, Role: RoleAdmin}, -1, 0, ErrInvalidWorkspace},
		{"instance default", Principal{Role: RoleAdmin, Global: true}, 0, 1, nil},
		{"instance explicit", Principal{Role: RoleOperator, Global: true}, 10, 10, nil},
		{"missing identity", Principal{}, 9, 0, ErrUnauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := withPrincipal(httptest.NewRequest("GET", "/", nil), tc.principal)
			id, err := Workspace(r.Context(), tc.requested)
			if id != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("workspace=%d err=%v", id, err)
			}
		})
	}
	r := withPrincipal(httptest.NewRequest("POST", "/", nil), Principal{UserID: 7, WorkspaceID: 9, Role: RoleViewer})
	w := httptest.NewRecorder()
	RequireRole(RoleAdmin, RoleOperator)(func(http.ResponseWriter, *http.Request) { t.Fatal("viewer reached write handler") })(w, r)
	if w.Code != 403 {
		t.Fatalf("viewer status=%d", w.Code)
	}
}

func TestSessionWritesRejectCrossOrigin(t *testing.T) {
	h := &AuthHandler{}
	for _, header := range []struct{ name, value string }{{"Origin", "https://other.example"}, {"Sec-Fetch-Site", "cross-site"}} {
		r := httptest.NewRequest("POST", "https://maild.example/v1/messages", nil)
		r.Header.Set(header.name, header.value)
		w := httptest.NewRecorder()
		h.RequireAuth(func(http.ResponseWriter, *http.Request) { t.Fatal("cross-origin reached handler") })(w, r)
		if w.Code != 403 {
			t.Fatalf("%s status=%d", header.name, w.Code)
		}
	}
}
