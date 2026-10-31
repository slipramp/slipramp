package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ListPromotionReviews(
	ctx context.Context,
	filter promotion.PromotionListFilter,
) (promotion.PromotionReviewPage, error) {
	// Keep the total and requested page consistent while Promotions transition.
	transaction, err := d.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return promotion.PromotionReviewPage{}, fmt.Errorf("begin Promotion review read: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	countParams := storedb.CountPromotionReviewsParams{
		RepositoryID: optionalDatabaseUUID(filter.RepositoryID),
		CommitSha:    nullableText(filter.CommitSHA),
		Status:       nullableText(string(filter.Status)),
		Integration:  nullableText(filter.Integration),
		StageID:      optionalDatabaseUUID(filter.StageID),
		ClusterName:  nullableText(filter.Cluster),
		Project:      nullableText(filter.Project),
		Application:  nullableText(filter.Application),
	}
	total, err := queries.CountPromotionReviews(ctx, countParams)
	if err != nil {
		return promotion.PromotionReviewPage{}, fmt.Errorf("count Promotion reviews: %w", err)
	}

	rows, err := queries.ListPromotionReviews(ctx, storedb.ListPromotionReviewsParams{
		RepositoryID: countParams.RepositoryID,
		CommitSha:    countParams.CommitSha,
		Status:       countParams.Status,
		Integration:  countParams.Integration,
		StageID:      countParams.StageID,
		ClusterName:  countParams.ClusterName,
		Project:      countParams.Project,
		Application:  countParams.Application,
		ResultLimit:  int32(filter.Limit),
		ResultOffset: int64(filter.Offset),
	})
	if err != nil {
		return promotion.PromotionReviewPage{}, fmt.Errorf("list Promotion reviews: %w", err)
	}

	items := make([]promotion.Promotion, 0, len(rows))
	for _, row := range rows {
		record, err := promotionFromRow(row)
		if err != nil {
			return promotion.PromotionReviewPage{}, err
		}
		items = append(items, record)
	}

	if err := transaction.Commit(ctx); err != nil {
		return promotion.PromotionReviewPage{}, fmt.Errorf("commit Promotion review read: %w", err)
	}

	return promotion.PromotionReviewPage{Items: items, Total: int(total)}, nil
}

func (d *Database) CountPendingPromotionsByStage(
	ctx context.Context,
	stageIDs []uuid.UUID,
) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(stageIDs))
	if len(stageIDs) == 0 {
		return counts, nil
	}
	identifiers := make([]pgtype.UUID, 0, len(stageIDs))
	for _, stageID := range stageIDs {
		identifiers = append(identifiers, databaseUUID(stageID))
	}
	rows, err := d.queries.CountPendingPromotionsByStage(ctx, identifiers)
	if err != nil {
		return nil, fmt.Errorf("count pending Promotions by Stage: %w", err)
	}
	for _, row := range rows {
		counts[uuid.UUID(row.StageID.Bytes)] = int(row.PendingPromotionCount)
	}

	return counts, nil
}
