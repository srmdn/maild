package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

type SessionStore struct {
	redis  *redis.Client
	prefix string
}

func NewSessionStore(redisAddr string, redisDB int) *SessionStore {
	rc := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   redisDB,
	})
	return &SessionStore{
		redis:  rc,
		prefix: "session:",
	}
}

func (s *SessionStore) Close() error {
	return s.redis.Close()
}

func (s *SessionStore) Create(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return "", err
	}
	key := s.prefix + sessionID
	expiresAt := time.Now().Add(ttl).Unix()
	err = s.redis.HSet(ctx, key,
		"user_id", userID,
		"expires_at", expiresAt,
	).Err()
	if err != nil {
		return "", err
	}
	if err = s.redis.Expire(ctx, key, ttl).Err(); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (s *SessionStore) Get(ctx context.Context, sessionID string) (int64, error) {
	key := s.prefix + sessionID
	vals, err := s.redis.HMGet(ctx, key, "user_id", "expires_at").Result()
	if err != nil {
		return 0, err
	}
	if vals[0] == nil {
		return 0, ErrSessionNotFound
	}
	userID, err := strconv.ParseInt(vals[0].(string), 10, 64)
	if err != nil {
		return 0, ErrSessionNotFound
	}
	if vals[1] != nil {
		expiresAt, err := strconv.ParseInt(vals[1].(string), 10, 64)
		if err == nil && time.Now().Unix() > expiresAt {
			_ = s.redis.Del(ctx, key).Err()
			return 0, ErrSessionExpired
		}
	}
	return userID, nil
}

func (s *SessionStore) Delete(ctx context.Context, sessionID string) error {
	key := s.prefix + sessionID
	return s.redis.Del(ctx, key).Err()
}

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
