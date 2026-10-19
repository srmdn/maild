package queue

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// MAILD_TEST_REDIS_ADDR must point to an isolated Redis instance. Tests use
// unique keys and never flush the selected database.
func newTestRedisQueue(t *testing.T) *RedisQueue {
	t.Helper()
	addr := os.Getenv("MAILD_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("MAILD_TEST_REDIS_ADDR not set; skipping queue integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q, err := NewRedis(ctx, addr, 0)
	if err != nil {
		t.Fatalf("connect test Redis: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	q.key = "maild:test:queue:" + suffix
	q.retryKey = "maild:test:retry:" + suffix
	q.fairKey = "maild:test:fair:" + suffix
	t.Cleanup(func() {
		_ = q.client.Del(context.Background(), q.key, q.retryKey, q.fairKey).Err()
	})
	return q
}

func TestRedisQueueReadyMessagesAreFIFO(t *testing.T) {
	q := newTestRedisQueue(t)
	ctx := context.Background()
	for _, id := range []int64{901, 902, 903} {
		if err := q.Enqueue(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []int64{901, 902, 903} {
		got, ok, err := q.Dequeue(ctx, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got != want {
			t.Fatalf("Dequeue() = (%d, %t), want (%d, true)", got, ok, want)
		}
	}
}

func TestRedisQueueAlternatesReadyAndDueRetries(t *testing.T) {
	q := newTestRedisQueue(t)
	ctx := context.Background()
	for _, id := range []int64{1, 2, 3} {
		if err := q.Enqueue(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	readyAt := time.Now().Add(-time.Second)
	for _, id := range []int64{901, 902} {
		if err := q.ScheduleRetry(ctx, id, readyAt); err != nil {
			t.Fatal(err)
		}
	}

	for _, want := range []int64{901, 1, 902, 2, 3} {
		got, ok, err := q.Dequeue(ctx, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got != want {
			t.Fatalf("Dequeue() = (%d, %t), want (%d, true)", got, ok, want)
		}
	}
}

func TestRedisQueueMakesProgressUnderSustainedIngress(t *testing.T) {
	q := newTestRedisQueue(t)
	ctx := context.Background()
	ready := make([]int64, 20)
	retries := make([]int64, 20)
	for i := range ready {
		ready[i] = int64(1000 + i)
		retries[i] = int64(2000 + i)
		if err := q.Enqueue(ctx, ready[i]); err != nil {
			t.Fatal(err)
		}
		if err := q.ScheduleRetry(ctx, retries[i], time.Now().Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	stop := make(chan struct{})
	ingressDemand := make(chan struct{})
	ingressAck := make(chan error)
	go func() {
		for i := int64(0); ; i++ {
			select {
			case <-stop:
				return
			case <-ingressDemand:
			}
			ingressAck <- q.Enqueue(ctx, 3000+i)
		}
	}()
	defer close(stop)
	requestIngress := func() {
		ingressDemand <- struct{}{}
		if err := <-ingressAck; err != nil {
			t.Fatalf("sustained ingress failed: %v", err)
		}
	}
	requestIngress()

	want := make(map[int64]bool, len(ready)+len(retries))
	for _, id := range ready {
		want[id] = true
	}
	for _, id := range retries {
		want[id] = true
	}
	seen := make(map[int64]bool, len(want))
	for i := 0; i < len(want); i++ {
		id, ok, err := q.Dequeue(ctx, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("queue made no progress at dequeue %d", i)
		}
		if want[id] {
			seen[id] = true
		}
		requestIngress()
	}
	for id := range want {
		if !seen[id] {
			t.Fatalf("backlogged message %d starved during sustained ingress", id)
		}
	}
}
