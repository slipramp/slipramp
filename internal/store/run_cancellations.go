package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/cancellation"
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
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin cancel Run transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	record, err := cancelRunInTransaction(ctx, d.queries.WithTx(transaction), runID, requestedAt, canceledBy)
	if err != nil {
		return run.Run{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit Run cancellation: %w", err)
	}
	return record, nil
}

func cancelRunInTransaction(
	ctx context.Context,
	queries *storedb.Queries,
	runID uuid.UUID,
	requestedAt time.Time,
	canceledBy *productidentity.ActorSnapshot,
) (run.Run, error) {
	canceledByPrincipalID, canceledBySnapshot, err := actorDatabaseFields(canceledBy)
	if err != nil {
		return run.Run{}, fmt.Errorf("encode Run canceler: %w", err)
	}

	current, err := queries.GetRunForCancel(ctx, databaseUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrRunNotFound
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get Run to cancel: %w", err)
	}

	currentRecord, err := runFromRow(current)
	if err != nil {
		return run.Run{}, err
	}
	transition, err := run.PlanCancellationRequest(currentRecord.CancellationStatus(), requestedAt)
	if err != nil {
		return run.Run{}, err
	}
	if transition.Operation == run.CancellationOperationNone {
		return currentRecord, nil
	}

	var updated storedb.Run
	switch transition.Operation {
	case run.CancellationOperationRequestActive:
		updated, err = queries.RequestActiveRunCancellation(ctx, storedb.RequestActiveRunCancellationParams{
			RequestedAt:           databaseTime(requestedAt),
			CanceledByPrincipalID: canceledByPrincipalID,
			CanceledBySnapshot:    canceledBySnapshot,
			ID:                    databaseUUID(runID),
		})
	case run.CancellationOperationFinishQueued:
		updated, err = queries.FinishQueuedRunCancellation(ctx, storedb.FinishQueuedRunCancellationParams{
			RequestedAt:           databaseTime(requestedAt),
			CanceledByPrincipalID: canceledByPrincipalID,
			CanceledBySnapshot:    canceledBySnapshot,
			ID:                    databaseUUID(runID),
		})
	default:
		return run.Run{}, fmt.Errorf(
			"%w: unsupported cancellation operation %q",
			run.ErrInvalidCancellation,
			transition.Operation,
		)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrInvalidCancellation
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("cancel Run: %w", err)
	}

	return runFromRow(updated)
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
	state, err := record.CancellationState()
	if err != nil {
		return run.Run{}, err
	}
	if state != cancellation.Requested {
		return run.Run{}, fmt.Errorf(
			"%w: cancellation command selected Run in state %q",
			run.ErrInvalidCancellation,
			state,
		)
	}

	return record, nil
}
