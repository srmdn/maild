package queue

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	client *redis.Client
	key    string
}

func NewRedis(ctx context.Context, addr string, db int) (*RedisQueue, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   db,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &RedisQueue{client: client, key: "maild:queue:messages"}, nil
}

func (q *RedisQueue) Close() error {
	return q.client.Close()
}

func (q *RedisQueue) Client() *redis.Client {
	return q.client
}

func (q *RedisQueue) Check(ctx context.Context) bool {
	ctxPing, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return q.client.Ping(ctxPing).Err() == nil
}

func (q *RedisQueue) Enqueue(ctx context.Context, id int64) error {
	return q.client.RPush(ctx, q.key, id).Err()
}

func (q *RedisQueue) Dequeue(ctx context.Context, timeout time.Duration) (int64, bool, error) {
	result, err := q.client.BRPop(ctx, timeout, q.key).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if len(result) != 2 {
		return 0, false, nil
	}
	id, err := strconv.ParseInt(result[1], 10, 64)
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}
