package runtime

import (
	"context"
	"sync/atomic"
)

type DependencyState struct {
	postgresReady atomic.Bool
	redisReady    atomic.Bool
}

func NewDependencyState() *DependencyState {
	s := &DependencyState{}
	s.postgresReady.Store(false)
	s.redisReady.Store(false)
	return s
}

func (s *DependencyState) SetPostgresReady(v bool) {
	s.postgresReady.Store(v)
}

func (s *DependencyState) SetRedisReady(v bool) {
	s.redisReady.Store(v)
}

func (s *DependencyState) Snapshot() (postgres, redis bool, ready bool) {
	postgres = s.postgresReady.Load()
	redis = s.redisReady.Load()
	ready = postgres && redis
	return
}

type DependencyChecker interface {
	Check(context.Context) bool
}
