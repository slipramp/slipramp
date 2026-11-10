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
) (map[uuid.UUID]promotion.PromotionPolicySnapshot, error) {
	policies, err := store.ListPromotionStagePolicies(ctx, stageIDs)
	if err != nil {
		return nil, err
	}

	views := make(map[uuid.UUID]promotion.PromotionPolicySnapshot, len(stageIDs))
	for _, stageID := range stageIDs {
		policy, ok := policies[stageID]
		if !ok {
			views[stageID] = promotion.SnapshotStagePolicy(nil, at)
			continue
		}
		views[stageID] = promotion.SnapshotStagePolicy(&policy, at)
	}

	return views, nil
}
