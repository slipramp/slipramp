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
	Status StageCapabilityStatus
	Reason string
}

type StageCapabilityStatus string

const (
	StageCapabilityReady         StageCapabilityStatus = "ready"
	StageCapabilityNotConfigured StageCapabilityStatus = "not-configured"
	StageCapabilityNotApplicable StageCapabilityStatus = "not-applicable"
	StageCapabilityUnavailable   StageCapabilityStatus = "unavailable"
)

const controlFlowPromotionReason = "Control-flow Stages do not materialize Promotions."
const staleObservationPromotionReason = "Promotion is unavailable until the Kargo Stage inventory is current."

func stageCapabilities(
	stage promotion.Stage,
	integration promotion.IntegrationConfig,
	activeRepositoryURL string,
	observationStale bool,
) StageCapabilities {
	native := StageCapability{Status: StageCapabilityReady}
	if stage.ControlFlow {
		native.Status = StageCapabilityNotApplicable
		native.Reason = controlFlowPromotionReason
	} else if observationStale {
		native.Status = StageCapabilityUnavailable
		native.Reason = staleObservationPromotionReason
	}

	enrichment := stage.Enrichment()
	repositoryMatches := activeRepositoryURL != "" && activeRepositoryURL == stage.RepositoryURL
	review := StageCapability{Status: StageCapabilityReady}
	switch {
	case stage.ControlFlow:
		review.Status = StageCapabilityNotApplicable
		review.Reason = controlFlowPromotionReason
	case !integration.ReviewConfigured() || enrichment == nil:
		review.Status = StageCapabilityNotConfigured
		review.Reason = "Slipramp review is not configured for this Stage."
	case !repositoryMatches || !integration.CanReview(stage):
		review.Status = StageCapabilityUnavailable
		review.Reason = "Review requires application, environment, a matching allowed Repository, target branch, " +
			"source paths, and review configuration."
	}

	drift := StageCapability{Status: StageCapabilityReady}
	switch {
	case !integration.ReviewConfigured() || !hasBranchDriftEnrichment(stage):
		drift.Status = StageCapabilityNotConfigured
		drift.Reason = "Branch drift is not configured for this Stage."
	case !repositoryMatches || !hasBranchDriftMapping(stage, integration):
		drift.Status = StageCapabilityUnavailable
		drift.Reason = "Branch drift requires a matching allowed Repository, target branch, and source paths."
	}

	return StageCapabilities{Promotion: native, Review: review, BranchDrift: drift}
}

func hasBranchDriftEnrichment(stage promotion.Stage) bool {
	return stage.RepositoryName != "" || stage.RepositoryURL != "" || stage.TargetBranch != "" ||
		len(stage.SourcePaths) > 0
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
