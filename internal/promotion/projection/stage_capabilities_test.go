package projection

import (
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestStageCapabilitiesDistinguishOptionalEnrichmentStates(t *testing.T) {
	t.Parallel()

	integration := promotion.IntegrationConfig{
		ClusterName:  "colo",
		Repositories: []string{"ops"},
		Preparation: promotion.PreparationConfig{
			Repository: "ops",
			Target:     "app",
			Action:     "prepare",
		},
	}
	nativeOnly := projectionTestStage(
		"prod",
		"colo",
		"kargo-core",
		"native",
		time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC),
	)
	nativeOnly.Application = ""
	nativeOnly.Environment = ""
	nativeOnly.RepositoryName = ""
	nativeOnly.RepositoryURL = ""
	nativeOnly.TargetBranch = ""
	nativeOnly.SourcePaths = nil

	nativeCapabilities := stageCapabilities(nativeOnly, integration, "", false)
	if nativeCapabilities.Promotion.Status != StageCapabilityReady ||
		nativeCapabilities.Review.Status != StageCapabilityNotConfigured ||
		nativeCapabilities.BranchDrift.Status != StageCapabilityNotConfigured {
		t.Errorf("native-only capabilities = %#v", nativeCapabilities)
	}

	partial := nativeOnly
	partial.Application = "native"
	partialCapabilities := stageCapabilities(partial, integration, "", false)
	if partialCapabilities.Review.Status != StageCapabilityUnavailable ||
		partialCapabilities.BranchDrift.Status != StageCapabilityNotConfigured {
		t.Errorf("partially enriched capabilities = %#v", partialCapabilities)
	}

	broken := projectionTestStage("prod", "colo", "kargo-core", "broken", nativeOnly.LastObservedAt)
	brokenCapabilities := stageCapabilities(
		broken,
		integration,
		"https://example.com/replacement.git",
		false,
	)
	if brokenCapabilities.Review.Status != StageCapabilityUnavailable ||
		brokenCapabilities.BranchDrift.Status != StageCapabilityUnavailable {
		t.Errorf("regressed capabilities = %#v", brokenCapabilities)
	}
}
