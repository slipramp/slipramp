package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ReleasePromotionGates(
	ctx context.Context,
	promotionID uuid.UUID,
	evaluatorProcessID uuid.UUID,
	releasedAt time.Time,
) (bool, error) {
	if promotionID == uuid.Nil || evaluatorProcessID == uuid.Nil || releasedAt.IsZero() {
		return false, fmt.Errorf("%w: Promotion Gate release context is incomplete", promotion.ErrInvalid)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin Promotion Gate release: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	row, err := queries.LockPromotion(ctx, databaseUUID(promotionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, promotion.ErrPromotionNotFound
	}
	if err != nil {
		return false, fmt.Errorf("lock Promotion for Gate release: %w", err)
	}
	record, err := promotionFromRow(row)
	if err != nil {
		return false, err
	}
	if record.Lifecycle.State != promotion.LifecycleApproved {
		return false, promotion.ErrTransitionConflict
	}

	if _, err := queries.LockPromotionGateRepositories(ctx, databaseUUID(promotionID)); err != nil {
		return false, fmt.Errorf("lock Promotion Gate repositories: %w", err)
	}
	if _, err := queries.ReleasePromotionGates(
		ctx,
		storedb.ReleasePromotionGatesParams{
			PromotionID:        databaseUUID(promotionID),
			EvaluatorProcessID: databaseUUID(evaluatorProcessID),
			ReleasedAt:         databaseTime(releasedAt),
		},
	); err != nil {
		return false, fmt.Errorf("release Promotion Gates: %w", err)
	}

	unreleased, err := queries.CountUnreleasedPromotionGates(ctx, databaseUUID(promotionID))
	if err != nil {
		return false, fmt.Errorf("count unreleased Promotion Gates: %w", err)
	}
	if unreleased != 0 {
		return false, nil
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit Promotion Gate release: %w", err)
	}

	return true, nil
}
