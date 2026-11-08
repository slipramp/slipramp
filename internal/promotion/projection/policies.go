package projection

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
)

type StagePolicyStore interface {
	ListPromotionStagePolicies(context.Context, []uuid.UUID) (map[uuid.UUID]promotion.StagePolicy, error)
}

func stagePolicyViews(
	ctx context.Context,
	store StagePolicyStore,
	stageIDs []uuid.UUID,
	at time.Time,
) (map[uuid.UUID]promotion.PolicyView, error) {
	policies, err := store.ListPromotionStagePolicies(ctx, stageIDs)
	if err != nil {
		return nil, err
	}

	views := make(map[uuid.UUID]promotion.PolicyView, len(stageIDs))
	for _, stageID := range stageIDs {
		policy, ok := policies[stageID]
		if !ok {
			views[stageID] = policyView(nil, at)
			continue
		}
		views[stageID] = policyView(&policy, at)
	}

	return views, nil
}

func policyView(record *promotion.StagePolicy, at time.Time) promotion.PolicyView {
	if record == nil {
		return promotion.PolicyView{
			ConfiguredMode: promotion.PolicyModeAutomatic,
			EffectiveMode:  promotion.PolicyModeAutomatic,
		}
	}

	updatedBy := record.UpdatedBySnapshot
	updatedAt := record.UpdatedAt

	return promotion.PolicyView{
		ConfiguredMode: record.Mode,
		EffectiveMode:  record.Mode.Effective(record.ExpiresAt, at),
		ExpiresAt:      record.ExpiresAt,
		Reason:         record.Reason,
		UpdatedBy:      &updatedBy,
		UpdatedAt:      &updatedAt,
	}
}
