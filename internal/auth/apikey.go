package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/srmdn/maild/internal/crypto"
	"github.com/srmdn/maild/internal/domain"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type ctxKey string

const principalContextKey ctxKey = "auth_principal"

// Principal distinguishes trusted instance keys from workspace-scoped users.
// Only authentication middleware may grant Global access.
type Principal struct {
	UserID      int64
	WorkspaceID int64
	Role        Role
	Global      bool
}

type APIKeyStore interface {
	AuthenticateAPIKey(context.Context, string) (domain.APIKeyIdentity, error)
}

func withPrincipal(r *http.Request, p Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalContextKey, p))
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalContextKey).(Principal)
	return p, ok && validRole(p.Role) && (p.Global || (p.UserID > 0 && p.WorkspaceID > 0))
}

func validRole(role Role) bool {
	return role == RoleAdmin || role == RoleOperator || role == RoleViewer
}

func APIKeyMiddleware(headerName, adminKey, operatorKey string, store APIKeyStore, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(r.Header.Get(headerName))
		if role, ok := resolveRole(got, adminKey, operatorKey); ok {
			next(w, withPrincipal(r, Principal{Role: role, Global: true}))
			return
		}
		if got == "" || store == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		identity, err := store.AuthenticateAPIKey(r.Context(), crypto.HashAPIKey(got))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
			} else {
				writeError(w, http.StatusInternalServerError, "authentication unavailable")
			}
			return
		}
		r = withPrincipal(r, Principal{UserID: identity.UserID, WorkspaceID: identity.WorkspaceID, Role: Role(identity.Role)})
		if _, ok := PrincipalFromContext(r.Context()); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

var (
	ErrUnauthenticated  = errors.New("unauthenticated")
	ErrForbidden        = errors.New("forbidden")
	ErrInvalidWorkspace = errors.New("invalid workspace")
)

// Workspace defaults user requests to their authenticated workspace. Global keys
// retain their explicit, trusted cross-workspace capability and legacy default.
func Workspace(ctx context.Context, requested int64) (int64, error) {
	p, ok := PrincipalFromContext(ctx)
	if !ok {
		return 0, ErrUnauthenticated
	}
	if requested < 0 {
		return 0, ErrInvalidWorkspace
	}
	if p.Global {
		if requested == 0 {
			return 1, nil
		}
		return requested, nil
	}
	if requested == 0 {
		return p.WorkspaceID, nil
	}
	if requested != p.WorkspaceID {
		return 0, ErrForbidden
	}
	return requested, nil
}

func RequireRole(allowed ...Role) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			role, ok := RoleFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			for _, a := range allowed {
				if role == a {
					next(w, r)
					return
				}
			}
			writeError(w, http.StatusForbidden, "forbidden")
		}
	}
}

func RoleFromContext(ctx context.Context) (Role, bool) {
	p, ok := PrincipalFromContext(ctx)
	return p.Role, ok
}

func resolveRole(provided, adminKey, operatorKey string) (Role, bool) {
	if provided == "" {
		return "", false
	}
	if adminKey != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(adminKey)) == 1 {
		return RoleAdmin, true
	}
	if operatorKey != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(operatorKey)) == 1 {
		return RoleOperator, true
	}
	return "", false
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
