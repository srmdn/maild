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
	"github.com/srmdn/maild/internal/httpserver"
	"github.com/srmdn/maild/internal/queue"
	"github.com/srmdn/maild/internal/service"
	"github.com/srmdn/maild/internal/smtpclient"
	"github.com/srmdn/maild/internal/store/postgres"
	"github.com/srmdn/maild/internal/worker"
)

func Run() error {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
	ctx := context.Background()
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	store, err := postgres.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer store.Close()

	msgQueue, err := queue.NewRedis(ctx, cfg.RedisAddr, cfg.RedisDB)
	if err != nil {
		return err
	}
	defer msgQueue.Close()

	sender := smtpclient.New(cfg)
	messageService := service.NewMessageService(store, msgQueue, sender, cfg.MaxAttempts)
	if err := messageService.Bootstrap(ctx); err != nil {
		return err
	}

	apiHandler := api.NewHandler(messageService)
	server := httpserver.New(cfg, apiHandler)
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
