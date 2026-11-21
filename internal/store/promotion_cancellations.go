package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

// CancelPromotion records the request and cancels any linked preparation Run atomically.
func (d *Database) CancelPromotion(
	ctx context.Context,
	id uuid.UUID,
	at time.Time,
	actor productidentity.ActorSnapshot,
) (promotion.Promotion, string, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return promotion.Promotion{}, "", err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	queries := d.queries.WithTx(tx)
	row, err := queries.LockPromotion(ctx, databaseUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, "", promotion.ErrPromotionNotFound
	}
	if err != nil {
		return promotion.Promotion{}, "", err
	}
	record, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, "", err
	}
	if record.Cancellation.RequestedAt != nil {
		return record, "", nil
	}
	if !record.CanRequestCancellation(at) {
		return promotion.Promotion{}, "", promotion.ErrTransitionConflict
	}

	actorID, snapshot, err := actorDatabaseFields(&actor)
	if err != nil {
		return promotion.Promotion{}, "", err
	}

	var confirmedAt *time.Time
	if record.Source == promotion.SourceSlipramp && record.CreationDispatch.AttemptCount == 0 {
		confirmedAt = &at
	}
	row, err = queries.RequestPromotionCancellation(ctx, storedb.RequestPromotionCancellationParams{
		ID:            databaseUUID(id),
		RequestedAt:   databaseTime(at),
		ConfirmedAt:   optionalDatabaseTime(confirmedAt),
		ActorID:       actorID,
		ActorSnapshot: snapshot,
	})
	if err != nil {
		return promotion.Promotion{}, "", fmt.Errorf("record Promotion cancellation: %w", err)
	}

	var prepCluster string
	if record.Preparation.RunID != nil {
		prep, err := cancelRunInTransaction(ctx, queries, *record.Preparation.RunID, at, &actor)
		if err != nil && !errors.Is(err, run.ErrRunFinished) {
			return promotion.Promotion{}, "", err
		}
		prepCluster = prep.ClusterName
	}

	updated, err := promotionFromRow(row)
	if err != nil {
		return promotion.Promotion{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return promotion.Promotion{}, "", err
	}
	return updated, prepCluster, nil
}

func (d *Database) ClaimPromotionCancellation(
	ctx context.Context,
	cluster string,
	at, next time.Time,
) (promotion.Promotion, error) {
	row, err := d.queries.ClaimPromotionCancellation(ctx, storedb.ClaimPromotionCancellationParams{
		ClusterName:   cluster,
		DueAt:         databaseTime(at),
		NextAttemptAt: databaseTime(next),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.Promotion{}, promotion.ErrNoCancellation
	}
	if err != nil {
		return promotion.Promotion{}, err
	}
	return promotionFromRow(row)
}
