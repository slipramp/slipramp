package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

// A committed preparation Run always has its Promotion link, including when the
// caller loses the commit response.
func linkPromotionPreparationRun(
	ctx context.Context,
	queries *storedb.Queries,
	record run.Run,
	createdBySnapshot []byte,
) error {
	if record.Source.Type != run.SourceTypePromotion {
		return nil
	}
	if record.Source.Promotion == nil || record.Source.Promotion.PromotionID == uuid.Nil ||
		record.IdempotencyKey == nil || *record.IdempotencyKey != record.Source.Promotion.PromotionID.String() ||
		record.CreatedBy == nil {
		return fmt.Errorf("%w: Promotion preparation Run must use its requesting actor and identity", run.ErrInvalidRequest)
	}
	affected, err := queries.LinkPromotionPreparationRun(ctx, storedb.LinkPromotionPreparationRunParams{
		ID:                   databaseUUID(record.Source.Promotion.PromotionID),
		PreparationRunID:     databaseUUID(record.ID),
		RepositoryID:         databaseUUID(record.RepositoryID),
		CatalogRevisionID:    databaseUUID(record.CatalogRevisionID),
		CreatedByPrincipalID: databaseUUID(record.CreatedBy.PrincipalID),
		CreatedBySnapshot:    createdBySnapshot,
		CreatedAt:            databaseTime(record.CreatedAt),
	})
	if err != nil {
		return fmt.Errorf("link Promotion preparation Run: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: Promotion is not eligible for this preparation Run", run.ErrInvalidRequest)
	}

	return nil
}
