package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
)

type ctxKey string

const roleContextKey ctxKey = "auth_role"

func APIKeyMiddleware(headerName, adminKey, operatorKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(r.Header.Get(headerName))
		role, ok := resolveRole(got, adminKey, operatorKey)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), roleContextKey, role)
		next(w, r.WithContext(ctx))
	}
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
	v := ctx.Value(roleContextKey)
	role, ok := v.(Role)
	return role, ok
}

func resolveRole(provided, adminKey, operatorKey string) (Role, bool) {
	if subtle.ConstantTimeCompare([]byte(provided), []byte(adminKey)) == 1 {
		return RoleAdmin, true
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(operatorKey)) == 1 {
		return RoleOperator, true
	}
	return "", false
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
