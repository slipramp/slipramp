package projection

import (
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestStageHasNewerFreightRequiresReadyLaterDiscovery(t *testing.T) {
	t.Parallel()

	currentAt := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	newerAt := currentAt.Add(time.Hour)
	olderAt := currentAt.Add(-time.Hour)

	stage := ProjectStage{
		Stage: promotion.Stage{Freight: promotion.Freight{Items: []promotion.FreightItem{{
			Origin: "Warehouse/app", Name: "current", DiscoveredAt: &currentAt,
		}}}},
		Capabilities: StageCapabilities{Promotion: StageCapability{Status: StageCapabilityReady}},
	}
	stage.RequestedFreight = []ProjectFreightRequest{{Candidates: []FreightCandidate{{
		Freight:  promotion.FreightItem{Origin: "Warehouse/app", Name: "newer", DiscoveredAt: &newerAt},
		Evidence: promotion.FreightReadinessEvidence{Readiness: promotion.FreightReadinessReady},
	}}}}
	if !stageHasNewerFreight(stage) {
		t.Fatal("Stage with ready later Freight did not match")
	}

	stage.RequestedFreight[0].Candidates[0].Freight.DiscoveredAt = &olderAt
	if stageHasNewerFreight(stage) {
		t.Fatal("Stage with only ready rollback Freight matched")
	}

	stage.RequestedFreight[0].Candidates[0].Freight.DiscoveredAt = &newerAt
	stage.RequestedFreight[0].Candidates[0].Evidence.Readiness = promotion.FreightReadinessUnavailable
	if stageHasNewerFreight(stage) {
		t.Fatal("Stage with unavailable newer Freight matched")
	}

	stage.RequestedFreight[0].Candidates[0].Evidence.Readiness = promotion.FreightReadinessReady
	stage.RequestedFreight[0].Candidates[0].Freight.DiscoveredAt = nil
	if stageHasNewerFreight(stage) {
		t.Fatal("Stage with unknown candidate discovery time matched")
	}

	stage.Stage.Freight.Items = nil
	if !stageHasNewerFreight(stage) {
		t.Fatal("Stage without current Freight did not match its ready candidate")
	}

	stage.Capabilities.Promotion.Status = StageCapabilityUnavailable
	if stageHasNewerFreight(stage) {
		t.Fatal("non-promotable Stage matched")
	}
}
