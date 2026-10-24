package notification

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const DefaultRetentionInterval = time.Hour

type RetentionStore interface {
	DeleteTerminalNotificationHistoryBefore(context.Context, time.Time) (int64, error)
}

type RetentionReconciler struct {
	store           RetentionStore
	retentionPeriod time.Duration
	interval        time.Duration
	logger          *slog.Logger
	now             func() time.Time
}

func NewRetentionReconciler(
	store RetentionStore,
	retentionPeriod time.Duration,
	interval time.Duration,
	logger *slog.Logger,
) *RetentionReconciler {
	return &RetentionReconciler{
		store:           store,
		retentionPeriod: retentionPeriod,
		interval:        interval,
		logger:          logger.With("component", "notification_retention"),
		now:             time.Now,
	}
}

func (r *RetentionReconciler) Run(ctx context.Context) {
	r.reconcileAndLog(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcileAndLog(ctx)
		}
	}
}

func (r *RetentionReconciler) Reconcile(ctx context.Context) (int64, error) {
	deleted, err := r.store.DeleteTerminalNotificationHistoryBefore(
		ctx,
		r.now().UTC().Add(-r.retentionPeriod),
	)
	if err != nil {
		return 0, fmt.Errorf("delete retained notification history: %w", err)
	}

	return deleted, nil
}

func (r *RetentionReconciler) reconcileAndLog(ctx context.Context) {
	deleted, err := r.Reconcile(ctx)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("reconcile notification history", "error", err)
		}

		return
	}
	if deleted > 0 {
		r.logger.Info("reconciled notification history", "deleted_events", deleted)
	}
}
