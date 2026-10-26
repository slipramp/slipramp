package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) GetAssignedRunForCluster(ctx context.Context, clusterName string) (run.Run, error) {
	row, err := d.queries.GetAssignedRunForCluster(ctx, clusterName)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrNoRunAssignment
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get assigned Run for cluster: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func (d *Database) ClaimQueuedRun(ctx context.Context, clusterName string, claimedAt time.Time) (run.Run, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin Run claim transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	row, err := queries.ClaimQueuedRun(
		ctx,
		storedb.ClaimQueuedRunParams{
			AssignedAt:  databaseTime(claimedAt),
			ClusterName: clusterName,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) || isTargetConcurrencyConflict(err) {
		return run.Run{}, run.ErrNoRunAssignment
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("claim queued Run: %w", err)
	}

	if err := queries.CreateRunPhaseTransition(
		ctx,
		storedb.CreateRunPhaseTransitionParams{
			RunID:      databaseUUID(uuid.UUID(row.ID.Bytes)),
			Phase:      string(run.PhaseAssigned),
			RecordedAt: databaseTime(claimedAt),
		},
	); err != nil {
		return run.Run{}, fmt.Errorf("record assigned Run phase: %w", err)
	}
	if row.ActionName == "sync" {
		updated, err := queries.RecordTargetSyncAttempt(
			ctx,
			storedb.RecordTargetSyncAttemptParams{
				RunID:    row.ID,
				TargetID: row.TargetID,
			},
		)
		if err != nil {
			return run.Run{}, fmt.Errorf("record Target sync attempt: %w", err)
		}
		if updated != 1 {
			return run.Run{}, errors.New("record Target sync attempt: Target does not exist")
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit Run claim transaction: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func isTargetConcurrencyConflict(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) &&
		postgresError.ConstraintName == "runs_one_active_per_target"
}
