package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisLimiter struct {
	client                 *redis.Client
	workspacePerHour       int
	recipientDomainPerHour int
}

func NewRedisLimiter(client *redis.Client, workspacePerHour, recipientDomainPerHour int) *RedisLimiter {
	return &RedisLimiter{
		client:                 client,
		workspacePerHour:       workspacePerHour,
		recipientDomainPerHour: recipientDomainPerHour,
	}
}

func (l *RedisLimiter) Allow(ctx context.Context, workspaceID int64, recipientDomain string) (bool, string, error) {
	now := time.Now().UTC()
	hourKey := now.Format("2006010215")

	wsKey := fmt.Sprintf("maild:rl:ws:%d:%s", workspaceID, hourKey)
	wsCount, err := l.incrementWindow(ctx, wsKey)
	if err != nil {
		return false, "", err
	}
	if wsCount > int64(l.workspacePerHour) {
		return false, "workspace_hourly_limit", nil
	}

	domainKey := fmt.Sprintf("maild:rl:domain:%d:%s:%s", workspaceID, recipientDomain, hourKey)
	domainCount, err := l.incrementWindow(ctx, domainKey)
	if err != nil {
		return false, "", err
	}
	if domainCount > int64(l.recipientDomainPerHour) {
		return false, "recipient_domain_hourly_limit", nil
	}

	return true, "", nil
}

func (l *RedisLimiter) incrementWindow(ctx context.Context, key string) (int64, error) {
	count, err := l.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if count == 1 {
		_ = l.client.Expire(ctx, key, 2*time.Hour).Err()
	}
	return count, nil
}
