package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func APIKeyMiddleware(headerName, expectedKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(r.Header.Get(headerName))
		if subtle.ConstantTimeCompare([]byte(got), []byte(expectedKey)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next(w, r)
	}
}
