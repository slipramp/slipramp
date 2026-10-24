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
	row, err := d.queries.RecordPromotionPreparationFailure(ctx, storedb.RecordPromotionPreparationFailureParams{
		ID:                  databaseUUID(failure.PromotionID),
		PreparationRunID:    optionalDatabaseUUID(failure.PreparationRunID),
		FailedAt:            databaseTime(failure.FailedAt),
		Permanent:           failure.Permanent,
		Diagnostic:          failure.Diagnostic,
		ExhaustedMessage:    "Promotion preparation retry budget exhausted. " + failure.Diagnostic,
		FailureLimit:        promotion.PreparationFailureLimit,
		InitialDelaySeconds: promotion.PreparationInitialRetryDelay.Seconds(),
		MaxDelaySeconds:     promotion.PreparationMaximumRetryDelay.Seconds(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.PreparationFailureState{}, false, nil
	}
	if err != nil {
		return promotion.PreparationFailureState{}, false, fmt.Errorf("record Promotion preparation failure: %w", err)
	}

	return promotion.PreparationFailureState{
		Count:    int(row.PreparationFailureCount),
		Terminal: row.Status == string(promotion.StatusError),
	}, true, nil
}
