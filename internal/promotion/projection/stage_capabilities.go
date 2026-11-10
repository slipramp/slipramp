package projection

import (
	"context"
	"slices"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

type StageCapabilities struct {
	Promotion   StageCapability
	Review      StageCapability
	BranchDrift StageCapability
}

type StageCapability struct {
	Ready  bool
	Reason string
}

const controlFlowPromotionReason = "Control-flow Stages do not materialize Promotions."

func stageCapabilities(
	stage promotion.Stage,
	integration promotion.IntegrationConfig,
	activeRepositoryURL string,
) StageCapabilities {
	native := StageCapability{Ready: !stage.ControlFlow}
	if stage.ControlFlow {
		native.Reason = controlFlowPromotionReason
	}

	repositoryMatches := activeRepositoryURL != "" && activeRepositoryURL == stage.RepositoryURL
	review := StageCapability{Ready: !stage.ControlFlow && repositoryMatches && integration.CanReview(stage)}
	if stage.ControlFlow {
		review.Reason = controlFlowPromotionReason
	} else if !review.Ready {
		review.Reason = "Review requires application, environment, a matching allowed Repository, target branch, " +
			"source paths, and review configuration."
	}

	drift := StageCapability{Ready: repositoryMatches && hasBranchDriftMapping(stage, integration)}
	if !drift.Ready {
		drift.Reason = "Branch drift requires a matching allowed Repository, target branch, and source paths."
	}

	return StageCapabilities{Promotion: native, Review: review, BranchDrift: drift}
}

func hasBranchDriftMapping(stage promotion.Stage, integration promotion.IntegrationConfig) bool {
	return stage.ClusterName == integration.ClusterName && slices.Contains(integration.Repositories, stage.RepositoryName) &&
		stage.RepositoryURL != "" && stage.TargetBranch != "" && len(stage.SourcePaths) > 0
}

func activeRepositoriesByName(
	ctx context.Context,
	store promotion.RepositoryLister,
) (map[string]repository.Repository, error) {
	repositories, err := store.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]repository.Repository, len(repositories))
	for _, record := range repositories {
		byName[record.Name] = record
	}
	return byName, nil
}
