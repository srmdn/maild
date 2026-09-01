package queue

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	client   *redis.Client
	key      string
	retryKey string
}

func NewRedis(ctx context.Context, addr string, db int) (*RedisQueue, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   db,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &RedisQueue{client: client, key: "maild:queue:messages", retryKey: "maild:queue:retry"}, nil
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
	if id, ok, err := q.popReadyRetry(ctx); err != nil {
		return 0, false, err
	} else if ok {
		return id, true, nil
	}

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

// ScheduleRetry adds the message to a delayed retry set, ready at the given time.
func (q *RedisQueue) ScheduleRetry(ctx context.Context, id int64, at time.Time) error {
	return q.client.ZAdd(ctx, q.retryKey, redis.Z{
		Score:  float64(at.UnixMilli()),
		Member: strconv.FormatInt(id, 10),
	}).Err()
}

var popReadyRetryScript = redis.NewScript(`
local res = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, 1)
if #res == 0 then return nil end
redis.call('ZREM', KEYS[1], res[1])
return res[1]
`)

func (q *RedisQueue) popReadyRetry(ctx context.Context) (int64, bool, error) {
	val, err := popReadyRetryScript.Run(ctx, q.client, []string{q.retryKey}, time.Now().UnixMilli()).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if val == nil {
		return 0, false, nil
	}
	idStr, ok := val.(string)
	if !ok || idStr == "" {
		return 0, false, nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, false, nil
	}
	return id, true, nil
}
