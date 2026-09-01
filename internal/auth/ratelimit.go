package auth

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	loginFailKeyPrefix  = "maild:login:fail:"
	loginBlockKeyPrefix = "maild:login:block:"
)

// LoginRateLimiter throttles failed sign-in attempts per client IP.
//
// It is intentionally fail-open: if Redis is unavailable the login flow is
// not blocked, because sign-in cannot succeed without Redis in most
// deployments anyway (sessions are Redis-backed). Callers should treat the
// returned errors as non-fatal.
type LoginRateLimiter struct {
	redis         *redis.Client
	maxFailures   int
	failureWindow time.Duration
	lockout       time.Duration
}

func NewLoginRateLimiter(client *redis.Client, maxFailures int, failureWindow, lockout time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{
		redis:         client,
		maxFailures:   maxFailures,
		failureWindow: failureWindow,
		lockout:       lockout,
	}
}

// IsLocked reports whether the IP is currently locked out.
func (l *LoginRateLimiter) IsLocked(ctx context.Context, ip string) (bool, error) {
	if l == nil || l.redis == nil {
		return false, nil
	}
	n, err := l.redis.Exists(ctx, l.blockKey(ip)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RecordFailure counts one failed login for the IP and sets a lockout once
// the threshold is reached within the failure window.
func (l *LoginRateLimiter) RecordFailure(ctx context.Context, ip string) error {
	if l == nil || l.redis == nil {
		return nil
	}
	failKey := l.failKey(ip)
	n, err := l.redis.Incr(ctx, failKey).Result()
	if err != nil {
		return err
	}
	if n == 1 {
		_ = l.redis.Expire(ctx, failKey, l.failureWindow).Err()
	}
	if n >= int64(l.maxFailures) {
		// Lock the IP out and start a fresh failure window.
		_ = l.redis.Set(ctx, l.blockKey(ip), "1", l.lockout).Err()
		_ = l.redis.Del(ctx, failKey).Err()
	}
	return nil
}

// Reset clears failure and lockout state for the IP after a successful login.
func (l *LoginRateLimiter) Reset(ctx context.Context, ip string) error {
	if l == nil || l.redis == nil {
		return nil
	}
	_ = l.redis.Del(ctx, l.failKey(ip)).Err()
	_ = l.redis.Del(ctx, l.blockKey(ip)).Err()
	return nil
}

func (l *LoginRateLimiter) failKey(ip string) string {
	return loginFailKeyPrefix + ip
}

func (l *LoginRateLimiter) blockKey(ip string) string {
	return loginBlockKeyPrefix + ip
}

// clientIP returns the real client address for rate limiting. It prefers the
// first X-Forwarded-For entry, then X-Real-IP, then the direct peer address.
// The app is expected to run behind a single trusted reverse proxy (nginx)
// that sets these headers on the loopback bind.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
