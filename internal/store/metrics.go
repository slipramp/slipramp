package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/cluster"
)

type MetricsSnapshot struct {
	ActiveRuns                 map[string]int64
	PendingRunReviews          int64
	OldestEligibleRunCreatedAt *time.Time
	ConnectedControllers       int64
	ControllerLastSeenAt       *time.Time
	RepositoryRefreshStates    map[string]int64
	NotificationDeliveryStates map[string]int64
}

type DatabasePoolMetrics struct {
	AcquiredConnections int64
	IdleConnections     int64
	MaximumConnections  int64
	AcquireWaits        int64
	CanceledAcquires    int64
	AcquireDuration     time.Duration
}

func (d *Database) LoadMetricsSnapshot(ctx context.Context, at time.Time) (MetricsSnapshot, error) {
	transaction, err := d.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("begin metrics snapshot: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	queries := d.queries.WithTx(transaction)
	snapshot := MetricsSnapshot{
		ActiveRuns:                 make(map[string]int64),
		RepositoryRefreshStates:    make(map[string]int64),
		NotificationDeliveryStates: make(map[string]int64),
	}

	activeRuns, err := queries.CountActiveRunsByPhase(ctx)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("count active Runs: %w", err)
	}
	for _, row := range activeRuns {
		snapshot.ActiveRuns[row.Phase] = row.RunCount
	}

	snapshot.PendingRunReviews, err = queries.CountPendingRunReviews(ctx)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("count pending Run reviews: %w", err)
	}

	oldestEligible, err := queries.GetOldestEligibleRunCreatedAt(ctx)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("get oldest eligible Run: %w", err)
	}
	if oldestEligible.Valid {
		createdAt := oldestEligible.Time.UTC()
		snapshot.OldestEligibleRunCreatedAt = &createdAt
	}

	controllerMetrics, err := queries.GetControllerMetrics(
		ctx,
		databaseTime(at.Add(-cluster.ConnectionWindow)),
	)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("get Controller metrics: %w", err)
	}
	snapshot.ConnectedControllers = controllerMetrics.ConnectedCount
	if controllerMetrics.LastSeenAt.Valid {
		lastSeenAt := controllerMetrics.LastSeenAt.Time.UTC()
		snapshot.ControllerLastSeenAt = &lastSeenAt
	}

	repositoryStates, err := queries.CountRepositoryRefreshStates(ctx)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("count Repository refresh states: %w", err)
	}
	for _, row := range repositoryStates {
		snapshot.RepositoryRefreshStates[row.RefreshStatus] = row.RepositoryCount
	}

	notificationStates, err := queries.CountNotificationDeliveryStates(ctx)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("count notification delivery states: %w", err)
	}
	for _, row := range notificationStates {
		snapshot.NotificationDeliveryStates[row.State] = row.DeliveryCount
	}

	if err := transaction.Commit(ctx); err != nil {
		return MetricsSnapshot{}, fmt.Errorf("commit metrics snapshot: %w", err)
	}

	return snapshot, nil
}

func (d *Database) DatabasePoolMetrics() DatabasePoolMetrics {
	statistics := d.pool.Stat()

	return DatabasePoolMetrics{
		AcquiredConnections: int64(statistics.AcquiredConns()),
		IdleConnections:     int64(statistics.IdleConns()),
		MaximumConnections:  int64(statistics.MaxConns()),
		AcquireWaits:        statistics.EmptyAcquireCount(),
		CanceledAcquires:    statistics.CanceledAcquireCount(),
		AcquireDuration:     statistics.AcquireDuration(),
	}
}
