package projection

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

type projectionTestStore struct {
	repositories  []repository.Repository
	stages        []promotion.Stage
	policies      map[uuid.UUID]promotion.StagePolicy
	pendingCounts map[uuid.UUID]int
}

func (s *projectionTestStore) ListRepositories(context.Context) ([]repository.Repository, error) {
	return s.repositories, nil
}

func (s *projectionTestStore) ListPromotionStages(context.Context) ([]promotion.Stage, error) {
	return s.stages, nil
}

func (s *projectionTestStore) ListPromotionStagePolicies(
	_ context.Context,
	stageIDs []uuid.UUID,
) (map[uuid.UUID]promotion.StagePolicy, error) {
	policies := make(map[uuid.UUID]promotion.StagePolicy, len(stageIDs))
	for _, stageID := range stageIDs {
		if policy, ok := s.policies[stageID]; ok {
			policies[stageID] = policy
		}
	}

	return policies, nil
}

func (s *projectionTestStore) CountPendingPromotionsByStage(
	_ context.Context,
	stageIDs []uuid.UUID,
) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(stageIDs))
	for _, stageID := range stageIDs {
		counts[stageID] = s.pendingCounts[stageID]
	}

	return counts, nil
}

func projectionTestStage(integration, cluster, project, name string, now time.Time) promotion.Stage {
	return promotion.Stage{
		ID:              uuid.New(),
		Integration:     integration,
		ClusterName:     cluster,
		Project:         project,
		Name:            name,
		Application:     name,
		Environment:     "prod",
		RepositoryName:  "ops",
		RepositoryURL:   "https://example.com/ops.git",
		TargetBranch:    "stage/" + name + "/prod",
		SourcePaths:     []string{"apps/" + name},
		FirstObservedAt: now,
		LastObservedAt:  now,
	}
}
