package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/srmdn/maild/internal/service"
)

type MessageWorker struct {
	svc *service.MessageService
	log *slog.Logger
}

func NewMessageWorker(svc *service.MessageService, log *slog.Logger) *MessageWorker {
	return &MessageWorker{svc: svc, log: log}
}

func (w *MessageWorker) Run(ctx context.Context) error {
	go w.runRecovery(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		id, ok, err := w.svc.PopQueue(ctx, 5*time.Second)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			w.log.Error("worker dequeue failed", "error", err)
			time.Sleep(1 * time.Second)
			continue
		}
		if !ok {
			continue
		}

		w.log.Info("worker_message_processing", "message_id", id)
		if err := w.svc.ProcessOne(ctx, id); err != nil {
			w.log.Error("worker processing failed", "message_id", id, "error", err)
			continue
		}
		w.log.Info("worker_message_processed", "message_id", id)
	}
}

func (w *MessageWorker) runRecovery(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	w.recoverMessages(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.recoverMessages(ctx)
		}
	}
}

func (w *MessageWorker) recoverMessages(ctx context.Context) {
	result, err := w.svc.RecoverQueue(ctx, 30*time.Second, 15*time.Minute, 100)
	if err != nil {
		w.log.Error("worker queue recovery failed", "error", err)
		return
	}
	if result.Requeued > 0 || result.MarkedUnknown > 0 {
		w.log.Warn("worker queue recovery completed", "requeued", result.Requeued, "unknown_delivery", result.MarkedUnknown)
	}
}
