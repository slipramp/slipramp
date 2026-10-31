package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/telemetry"
)

var _ telemetry.SnapshotSource = (*Database)(nil)

func (d *Database) LoadMetricsSnapshot(ctx context.Context, at time.Time) (telemetry.MetricsSnapshot, error) {
	transaction, err := d.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return telemetry.MetricsSnapshot{}, fmt.Errorf("begin metrics snapshot: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	queries := d.queries.WithTx(transaction)
	snapshot := telemetry.MetricsSnapshot{
		ActiveRuns:                 make(map[string]int64),
		RepositoryRefreshStates:    make(map[string]int64),
		NotificationDeliveryStates: make(map[string]int64),
	}

	activeRuns, err := queries.CountActiveRunsByPhase(ctx)
	if err != nil {
		return telemetry.MetricsSnapshot{}, fmt.Errorf("count active Runs: %w", err)
	}
	for _, row := range activeRuns {
		snapshot.ActiveRuns[row.Phase] = row.RunCount
	}

	snapshot.PendingRunReviews, err = queries.CountPendingRunReviews(ctx)
	if err != nil {
		return telemetry.MetricsSnapshot{}, fmt.Errorf("count pending Run reviews: %w", err)
	}

	oldestEligible, err := queries.GetOldestEligibleRunCreatedAt(ctx)
	if err != nil {
		return telemetry.MetricsSnapshot{}, fmt.Errorf("get oldest eligible Run: %w", err)
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
		return telemetry.MetricsSnapshot{}, fmt.Errorf("get Controller metrics: %w", err)
	}
	snapshot.ConnectedControllers = controllerMetrics.ConnectedCount
	if controllerMetrics.LastSeenAt.Valid {
		lastSeenAt := controllerMetrics.LastSeenAt.Time.UTC()
		snapshot.ControllerLastSeenAt = &lastSeenAt
	}

	repositoryStates, err := queries.CountRepositoryRefreshStates(ctx)
	if err != nil {
		return telemetry.MetricsSnapshot{}, fmt.Errorf("count Repository refresh states: %w", err)
	}
	for _, row := range repositoryStates {
		snapshot.RepositoryRefreshStates[row.RefreshStatus] = row.RepositoryCount
	}

	notificationStates, err := queries.CountNotificationDeliveryStates(ctx)
	if err != nil {
		return telemetry.MetricsSnapshot{}, fmt.Errorf("count notification delivery states: %w", err)
	}
	for _, row := range notificationStates {
		snapshot.NotificationDeliveryStates[row.State] = row.DeliveryCount
	}

	if err := transaction.Commit(ctx); err != nil {
		return telemetry.MetricsSnapshot{}, fmt.Errorf("commit metrics snapshot: %w", err)
	}

	return snapshot, nil
}

func (d *Database) DatabasePoolMetrics() telemetry.DatabasePoolMetrics {
	statistics := d.pool.Stat()

	return telemetry.DatabasePoolMetrics{
		AcquiredConnections: int64(statistics.AcquiredConns()),
		IdleConnections:     int64(statistics.IdleConns()),
		MaximumConnections:  int64(statistics.MaxConns()),
		AcquireWaits:        statistics.EmptyAcquireCount(),
		CanceledAcquires:    statistics.CanceledAcquireCount(),
		AcquireDuration:     statistics.AcquireDuration(),
	}
}
