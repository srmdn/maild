package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

type SessionStore struct {
	redis  *redis.Client
	ttl    time.Duration
	prefix string
}

func NewSessionStore(redisAddr string, redisDB int, sessionTTL time.Duration) *SessionStore {
	rc := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   redisDB,
	})
	return &SessionStore{
		redis:  rc,
		ttl:    sessionTTL,
		prefix: "session:",
	}
}

func (s *SessionStore) Close() error {
	return s.redis.Close()
}

func (s *SessionStore) Create(ctx context.Context, userID int64) (string, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return "", err
	}
	key := s.prefix + sessionID
	err = s.redis.HSet(ctx, key, "user_id", userID).Err()
	if err != nil {
		return "", err
	}
	err = s.redis.Expire(ctx, key, s.ttl).Err()
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

func (s *SessionStore) Get(ctx context.Context, sessionID string) (int64, error) {
	key := s.prefix + sessionID
	userID, err := s.redis.HGet(ctx, key, "user_id").Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, ErrSessionNotFound
		}
		return 0, err
	}
	_ = s.redis.Expire(ctx, key, s.ttl).Err()
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
