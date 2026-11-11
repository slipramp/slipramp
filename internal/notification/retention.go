package notification

import (
	"context"
	"fmt"
	"time"
)

const DefaultRetentionInterval = time.Hour

type RetentionStore interface {
	DeleteTerminalNotificationHistoryBefore(context.Context, time.Time) (int64, error)
}

type RetentionReconciler struct {
	store           RetentionStore
	retentionPeriod time.Duration
	now             func() time.Time
}

func NewRetentionReconciler(
	store RetentionStore,
	retentionPeriod time.Duration,
) *RetentionReconciler {
	return &RetentionReconciler{
		store:           store,
		retentionPeriod: retentionPeriod,
		now:             time.Now,
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
