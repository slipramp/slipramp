package store

import (
	"context"
	"fmt"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ promotion.StageReviewStore = (*Database)(nil)

func (d *Database) ListPromotionStageReviews(
	ctx context.Context,
	filter promotion.StageListFilter,
) (promotion.StageReviewPage, error) {
	countParams := storedb.CountPromotionStageReviewsParams{
		Integration: nullableText(filter.Integration),
		ClusterName: nullableText(filter.Cluster),
		Project:     nullableText(filter.Project),
		Application: nullableText(filter.Application),
		Archived:    filter.Archived,
	}
	total, err := d.queries.CountPromotionStageReviews(ctx, countParams)
	if err != nil {
		return promotion.StageReviewPage{}, fmt.Errorf("count Promotion Stage reviews: %w", err)
	}

	rows, err := d.queries.ListPromotionStageReviews(ctx, storedb.ListPromotionStageReviewsParams{
		Integration:  countParams.Integration,
		ClusterName:  countParams.ClusterName,
		Project:      countParams.Project,
		Application:  countParams.Application,
		Archived:     countParams.Archived,
		ResultLimit:  int32(filter.Limit),
		ResultOffset: int64(filter.Offset),
	})
	if err != nil {
		return promotion.StageReviewPage{}, fmt.Errorf("list Promotion Stage reviews: %w", err)
	}

	items := make([]promotion.StageReviewRecord, 0, len(rows))
	for _, row := range rows {
		record, err := promotionStageReviewFromRow(row)
		if err != nil {
			return promotion.StageReviewPage{}, err
		}
		items = append(items, record)
	}

	return promotion.StageReviewPage{Items: items, Total: int(total)}, nil
}

func promotionStageReviewFromRow(row storedb.ListPromotionStageReviewsRow) (promotion.StageReviewRecord, error) {
	stage, err := promotionStageFromRow(row.PromotionStage)
	if err != nil {
		return promotion.StageReviewRecord{}, err
	}
	record := promotion.StageReviewRecord{
		Stage:                 stage,
		PendingPromotionCount: int(row.PendingPromotionCount),
	}
	if !row.PolicyStageID.Valid {
		return record, nil
	}

	policy, err := promotionStagePolicyFromRow(storedb.PromotionStagePolicy{
		StageID:              row.PolicyStageID,
		Mode:                 textValue(row.PolicyMode),
		ExpiresAt:            row.PolicyExpiresAt,
		Reason:               row.PolicyReason,
		UpdatedByPrincipalID: row.PolicyUpdatedByPrincipalID,
		UpdatedBySnapshot:    row.PolicyUpdatedBySnapshot,
		UpdatedAt:            row.PolicyUpdatedAt,
	})
	if err != nil {
		return promotion.StageReviewRecord{}, err
	}
	record.Policy = &policy

	return record, nil
}
