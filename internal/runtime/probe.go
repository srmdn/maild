package runtime

import (
	"context"
	"log/slog"
	"time"
)

func StartDependencyProbe(ctx context.Context, logger *slog.Logger, state *DependencyState, postgres DependencyChecker, redis DependencyChecker, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	check := func() {
		pgReady := postgres.Check(ctx)
		rdReady := redis.Check(ctx)
		state.SetPostgresReady(pgReady)
		state.SetRedisReady(rdReady)
		logger.Info("dependency_probe", "postgres_ready", pgReady, "redis_ready", rdReady)
	}

	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}
