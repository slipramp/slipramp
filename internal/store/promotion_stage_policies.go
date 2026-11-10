package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) UpsertPromotionStagePolicy(
	ctx context.Context,
	record promotion.StagePolicy,
) (promotion.StagePolicy, error) {
	return upsertPromotionStagePolicy(ctx, d.queries, record)
}

func upsertPromotionStagePolicy(
	ctx context.Context,
	queries *storedb.Queries,
	record promotion.StagePolicy,
) (promotion.StagePolicy, error) {
	if err := record.Validate(); err != nil {
		return promotion.StagePolicy{}, err
	}

	actor, err := json.Marshal(record.UpdatedBySnapshot)
	if err != nil {
		return promotion.StagePolicy{}, fmt.Errorf("encode promotion policy actor: %w", err)
	}
	row, err := queries.UpsertPromotionStagePolicy(
		ctx,
		storedb.UpsertPromotionStagePolicyParams{
			StageID:              databaseUUID(record.StageID),
			Mode:                 string(record.Mode),
			ExpiresAt:            optionalDatabaseTime(record.ExpiresAt),
			Reason:               record.Reason,
			UpdatedByPrincipalID: databaseUUID(record.UpdatedByPrincipalID),
			UpdatedBySnapshot:    actor,
			UpdatedAt:            databaseTime(record.UpdatedAt),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if record.Mode == promotion.PolicyModeReviewRequired {
			return promotion.StagePolicy{}, promotion.ErrReviewContextUnavailable
		}
		return promotion.StagePolicy{}, promotion.ErrTransitionConflict
	}
	if err != nil {
		return promotion.StagePolicy{}, fmt.Errorf("upsert promotion Stage policy: %w", err)
	}

	return promotionStagePolicyFromRow(row)
}

func (d *Database) GetPromotionStagePolicy(
	ctx context.Context,
	stageID uuid.UUID,
) (promotion.StagePolicy, error) {
	return getPromotionStagePolicy(ctx, d.queries, stageID)
}

func getPromotionStagePolicy(
	ctx context.Context,
	queries *storedb.Queries,
	stageID uuid.UUID,
) (promotion.StagePolicy, error) {
	row, err := queries.GetPromotionStagePolicy(ctx, databaseUUID(stageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return promotion.StagePolicy{}, promotion.ErrPolicyNotFound
	}
	if err != nil {
		return promotion.StagePolicy{}, fmt.Errorf("get promotion Stage policy: %w", err)
	}

	return promotionStagePolicyFromRow(row)
}

func loadPromotionPolicySnapshot(
	ctx context.Context,
	queries *storedb.Queries,
	stageID uuid.UUID,
	at time.Time,
) (promotion.PromotionPolicySnapshot, error) {
	policy, err := getPromotionStagePolicy(ctx, queries, stageID)
	if errors.Is(err, promotion.ErrPolicyNotFound) {
		return promotion.SnapshotStagePolicy(nil, at), nil
	}
	if err != nil {
		return promotion.PromotionPolicySnapshot{}, err
	}

	return promotion.SnapshotStagePolicy(&policy, at), nil
}

func (d *Database) ListPromotionStagePolicies(
	ctx context.Context,
	stageIDs []uuid.UUID,
) (map[uuid.UUID]promotion.StagePolicy, error) {
	policies := make(map[uuid.UUID]promotion.StagePolicy, len(stageIDs))
	if len(stageIDs) == 0 {
		return policies, nil
	}

	identifiers := make([]pgtype.UUID, 0, len(stageIDs))
	for _, stageID := range stageIDs {
		identifiers = append(identifiers, databaseUUID(stageID))
	}
	rows, err := d.queries.ListPromotionStagePolicies(ctx, identifiers)
	if err != nil {
		return nil, fmt.Errorf("list Promotion Stage policies: %w", err)
	}
	for _, row := range rows {
		policy, decodeErr := promotionStagePolicyFromRow(row)
		if decodeErr != nil {
			return nil, decodeErr
		}
		policies[policy.StageID] = policy
	}

	return policies, nil
}

func promotionStagePolicyFromRow(row storedb.PromotionStagePolicy) (promotion.StagePolicy, error) {
	actor, err := decodeActor(row.UpdatedBySnapshot)
	if err != nil {
		return promotion.StagePolicy{}, fmt.Errorf("decode promotion policy actor: %w", err)
	}
	record := promotion.StagePolicy{
		StageID:              uuid.UUID(row.StageID.Bytes),
		Mode:                 promotion.PolicyMode(row.Mode),
		ExpiresAt:            applicationTime(row.ExpiresAt),
		Reason:               row.Reason,
		UpdatedByPrincipalID: uuid.UUID(row.UpdatedByPrincipalID.Bytes),
		UpdatedBySnapshot:    actor,
		UpdatedAt:            row.UpdatedAt.Time.UTC(),
	}

	return record, record.Validate()
}
