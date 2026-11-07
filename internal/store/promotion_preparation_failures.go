package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) RecordPromotionPreparationFailure(
	ctx context.Context,
	failure promotion.PreparationFailure,
) (promotion.PreparationFailureState, bool, error) {
	if failure.PromotionID == uuid.Nil || failure.FailedAt.IsZero() ||
		len(failure.Diagnostic) == 0 || len(failure.Diagnostic) > 512 {
		return promotion.PreparationFailureState{}, false, errors.New("invalid Promotion preparation failure")
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.PreparationFailureState{}, false, fmt.Errorf(
			"begin Promotion preparation failure report: %w",
			err,
		)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	lockedRow, err := queries.LockPromotion(ctx, databaseUUID(failure.PromotionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.PreparationFailureState{}, false, nil
	}
	if err != nil {
		return promotion.PreparationFailureState{}, false, fmt.Errorf("lock Promotion preparation failure: %w", err)
	}
	current, err := promotionFromRow(lockedRow)
	if err != nil {
		return promotion.PreparationFailureState{}, false, err
	}
	transition, err := promotion.TransitionPreparationFailure(
		current.Lifecycle,
		current.ReviewRequired,
		current.Preparation,
		failure,
	)
	if errors.Is(err, promotion.ErrTransitionConflict) {
		return promotion.PreparationFailureState{}, false, nil
	}
	if err != nil {
		return promotion.PreparationFailureState{}, false, err
	}
	if !transition.Changed {
		return promotion.PreparationFailureState{}, false, nil
	}

	row, err := queries.UpdatePromotionPreparationFailure(
		ctx,
		storedb.UpdatePromotionPreparationFailureParams{
			PreparationFailureCount:          int32(transition.State.FailureCount),
			NextPreparationAttemptAt:         optionalDatabaseTime(transition.State.NextAttemptAt),
			Status:                           string(transition.Lifecycle.State),
			PreparationCompletedAt:           optionalDatabaseTime(transition.State.CompletedAt),
			Message:                          transition.Message,
			UpdatedAt:                        databaseTime(failure.FailedAt),
			ID:                               databaseUUID(failure.PromotionID),
			ExpectedStatus:                   string(current.Lifecycle.State),
			ExpectedFailureCount:             int32(current.Preparation.FailureCount),
			ExpectedPreparationRunID:         optionalDatabaseUUID(current.Preparation.RunID),
			ExpectedNextPreparationAttemptAt: optionalDatabaseTime(current.Preparation.NextAttemptAt),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.PreparationFailureState{}, false, nil
	}
	if err != nil {
		return promotion.PreparationFailureState{}, false, fmt.Errorf("update Promotion preparation failure: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return promotion.PreparationFailureState{}, false, fmt.Errorf(
			"commit Promotion preparation failure report: %w",
			err,
		)
	}

	return promotion.PreparationFailureState{
		Count:    int(row.PreparationFailureCount),
		Terminal: row.Status == string(promotion.LifecycleError),
	}, true, nil
}
