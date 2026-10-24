package notification

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestRetentionReconcilerDeletesTerminalHistoryBeforeCutoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	store := &recordingRetentionStore{deleted: 3}
	reconciler := NewRetentionReconciler(
		store,
		30*24*time.Hour,
		DefaultRetentionInterval,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	reconciler.now = func() time.Time { return now }

	deleted, err := reconciler.Reconcile(t.Context())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if deleted != 3 || !store.before.Equal(now.Add(-30*24*time.Hour)) {
		t.Fatalf("deleted = %d, cutoff = %s", deleted, store.before)
	}
}

func TestRetentionReconcilerReturnsStoreFailure(t *testing.T) {
	t.Parallel()

	store := &recordingRetentionStore{err: errors.New("database unavailable")}
	reconciler := NewRetentionReconciler(
		store,
		DefaultRetentionPeriod,
		DefaultRetentionInterval,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if _, err := reconciler.Reconcile(t.Context()); err == nil {
		t.Fatal("Reconcile() error = nil")
	}
}

type recordingRetentionStore struct {
	before  time.Time
	deleted int64
	err     error
}

func (s *recordingRetentionStore) DeleteTerminalNotificationHistoryBefore(
	_ context.Context,
	before time.Time,
) (int64, error) {
	s.before = before

	return s.deleted, s.err
}
