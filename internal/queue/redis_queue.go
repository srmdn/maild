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
	fairKey  string
}

func NewRedis(ctx context.Context, addr string, db int) (*RedisQueue, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   db,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &RedisQueue{client: client, key: "maild:queue:messages", retryKey: "maild:queue:retry", fairKey: "maild:queue:last-source"}, nil
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
	// Push new work on the left; Dequeue pops from the right to preserve FIFO.
	return q.client.LPush(ctx, q.key, id).Err()
}

func (q *RedisQueue) Dequeue(ctx context.Context, timeout time.Duration) (int64, bool, error) {
	if id, ok, err := q.popNext(ctx); err != nil {
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

// popNext atomically selects work from the FIFO ready list and delayed retry
// set. When both are ready, it alternates sources so neither backlog starves.
var popNextScript = redis.NewScript(`
local retry = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ARGV[1], 'LIMIT', 0, 1)
local has_ready = redis.call('LLEN', KEYS[1]) > 0
local source = nil

if #retry > 0 and has_ready then
  local last = redis.call('GET', KEYS[3]) or 'ready'
  source = (last == 'retry') and 'ready' or 'retry'
elseif #retry > 0 then
  source = 'retry'
elseif has_ready then
  source = 'ready'
else
  return nil
end

redis.call('SET', KEYS[3], source)
if source == 'retry' then
  redis.call('ZREM', KEYS[2], retry[1])
  return retry[1]
end
return redis.call('RPOP', KEYS[1])
`)

func (q *RedisQueue) popNext(ctx context.Context) (int64, bool, error) {
	val, err := popNextScript.Run(ctx, q.client, []string{q.key, q.retryKey, q.fairKey}, time.Now().UnixMilli()).Result()
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
