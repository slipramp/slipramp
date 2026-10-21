package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) CancelRun(
	ctx context.Context,
	runID uuid.UUID,
	requestedAt time.Time,
	canceledBy *productidentity.ActorSnapshot,
) (run.Run, error) {
	canceledByPrincipalID, canceledBySnapshot, err := actorDatabaseFields(canceledBy)
	if err != nil {
		return run.Run{}, fmt.Errorf("encode Run canceler: %w", err)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin cancel Run transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	current, err := queries.GetRunForCancel(ctx, databaseUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrRunNotFound
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get Run to cancel: %w", err)
	}

	if current.Phase == string(run.PhaseFinished) {
		if !current.CancellationRequestedAt.Valid {
			return run.Run{}, run.ErrRunFinished
		}

		return commitCancelResult(ctx, transaction, current)
	}
	if current.CancellationRequestedAt.Valid {
		return commitCancelResult(ctx, transaction, current)
	}

	updated, err := queries.CancelRun(
		ctx,
		storedb.CancelRunParams{
			RequestedAt:           databaseTime(requestedAt),
			CanceledByPrincipalID: canceledByPrincipalID,
			CanceledBySnapshot:    canceledBySnapshot,
			ID:                    databaseUUID(runID),
		},
	)
	if err != nil {
		return run.Run{}, fmt.Errorf("cancel Run: %w", err)
	}

	if current.Phase == string(run.PhaseQueued) {
		outcome := string(run.OutcomeCanceled)
		reason := run.ReasonCancelRequested
		if err := queries.CreateRunPhaseTransition(
			ctx,
			storedb.CreateRunPhaseTransitionParams{
				RunID:      databaseUUID(runID),
				Phase:      string(run.PhaseFinished),
				Outcome:    &outcome,
				Reason:     &reason,
				RecordedAt: databaseTime(requestedAt),
			},
		); err != nil {
			return run.Run{}, fmt.Errorf("record canceled Run phase: %w", err)
		}
	}

	return commitCancelResult(ctx, transaction, updated)
}

func (d *Database) GetRunToCancelForCluster(ctx context.Context, clusterName string) (run.Run, error) {
	row, err := d.queries.GetRunToCancelForCluster(ctx, clusterName)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrNoRunToCancel
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get Run to cancel for cluster: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func commitCancelResult(ctx context.Context, transaction pgx.Tx, row storedb.Run) (run.Run, error) {
	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit cancel Run transaction: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}
