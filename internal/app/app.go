package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/srmdn/maild/internal/api"
	"github.com/srmdn/maild/internal/config"
	"github.com/srmdn/maild/internal/crypto"
	"github.com/srmdn/maild/internal/domaincheck"
	"github.com/srmdn/maild/internal/httpserver"
	"github.com/srmdn/maild/internal/migrate"
	"github.com/srmdn/maild/internal/queue"
	"github.com/srmdn/maild/internal/ratelimit"
	"github.com/srmdn/maild/internal/runtime"
	"github.com/srmdn/maild/internal/service"
	"github.com/srmdn/maild/internal/smtpclient"
	"github.com/srmdn/maild/internal/store/postgres"
	"github.com/srmdn/maild/internal/worker"
)

func Run() error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
	ctx := context.Background()
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	store, err := postgres.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := migrate.Up(ctx, store.DB()); err != nil {
		return err
	}

	msgQueue, err := queue.NewRedis(ctx, cfg.RedisAddr, cfg.RedisDB)
	if err != nil {
		return err
	}
	defer msgQueue.Close()

	sender := smtpclient.New(cfg)
	sealer, err := crypto.NewSealerFromBase64(cfg.EncryptionKeyB64)
	if err != nil {
		return err
	}
	limiter := ratelimit.NewRedisLimiter(msgQueue.Client(), cfg.RateLimitWorkspacePerHour, cfg.RateLimitDomainPerHour)
	messageService := service.NewMessageService(
		store,
		msgQueue,
		sender,
		sealer,
		limiter,
		cfg.BlockedRecipientDomains,
		cfg.MaxAttempts,
		cfg.WebhookApplyMaxAttempts,
		cfg.RateLimitWorkspacePerHour,
		cfg.RateLimitDomainPerHour,
		cfg.AutoFailoverEnabled,
		cfg.AutoFailoverFailures,
		cfg.AutoFailoverWindow,
		cfg.AutoFailoverCooldown,
	)
	if err := messageService.Bootstrap(ctx); err != nil {
		return err
	}
	domainService := service.NewDomainService(store, domaincheck.New())

	deps := runtime.NewDependencyState()
	apiHandler := api.NewHandler(
		messageService,
		domainService,
		cfg.AppEnv,
		cfg.APIKeyHeader,
		cfg.AdminAPIKey,
		cfg.OperatorAPIKey,
		cfg.WebhooksEnabled,
		cfg.WebhookSignatureHeader,
		cfg.WebhookTimestampHeader,
		cfg.WebhookSigningSecret,
		cfg.WebhookMaxSkew,
		logger,
	)
	server := httpserver.New(cfg, logger, deps, apiHandler)
	messageWorker := worker.NewMessageWorker(messageService, logger)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", cfg.Addr, "app_env", cfg.AppEnv)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		logger.Info("message worker started")
		if err := messageWorker.Run(appCtx); err != nil {
			errCh <- err
		}
	}()
	go runtime.StartDependencyProbe(appCtx, logger, deps, store, msgQueue, 5*time.Second)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
		appCancel()
	case err := <-errCh:
		appCancel()
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return err
	}

	logger.Info("server stopped", "shutdown_timeout", cfg.ShutdownTimeout.String())
	time.Sleep(10 * time.Millisecond)
	return nil
}
