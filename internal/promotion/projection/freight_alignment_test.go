package projection

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestFreightAlignmentsEvaluateExactCurrentFreightIdentity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		sourceFreight *promotion.FreightItem
		targetFreight *promotion.FreightItem
		sourceStale   bool
		targetStale   bool
		resolved      bool
		want          FreightAlignmentStatus
	}{
		{
			name:          "matching Freight identity",
			sourceFreight: freightAlignmentTestItem("freight-1"),
			targetFreight: freightAlignmentTestItem("freight-1"),
			resolved:      true,
			want:          FreightAlignmentAligned,
		},
		{
			name:          "different Freight identities",
			sourceFreight: freightAlignmentTestItem("freight-2"),
			targetFreight: freightAlignmentTestItem("freight-1"),
			resolved:      true,
			want:          FreightAlignmentDifferent,
		},
		{
			name:          "current Freight only on source",
			sourceFreight: freightAlignmentTestItem("freight-1"),
			resolved:      true,
			want:          FreightAlignmentDifferent,
		},
		{
			name:          "current Freight only on target",
			targetFreight: freightAlignmentTestItem("freight-1"),
			resolved:      true,
			want:          FreightAlignmentDifferent,
		},
		{
			name:     "neither Stage has current Freight",
			resolved: true,
			want:     FreightAlignmentUnknown,
		},
		{
			name:          "stale source observation",
			sourceFreight: freightAlignmentTestItem("freight-1"),
			targetFreight: freightAlignmentTestItem("freight-1"),
			sourceStale:   true,
			resolved:      true,
			want:          FreightAlignmentUnknown,
		},
		{
			name:          "stale target observation",
			sourceFreight: freightAlignmentTestItem("freight-1"),
			targetFreight: freightAlignmentTestItem("freight-1"),
			targetStale:   true,
			resolved:      true,
			want:          FreightAlignmentUnknown,
		},
		{
			name:          "unresolved source Stage",
			targetFreight: freightAlignmentTestItem("freight-1"),
			want:          FreightAlignmentUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			source := projectTestStage("prod", "colo", "kargo-core", "source", now)
			target := projectTestStage("prod", "colo", "kargo-core", "target", now)
			target.RequestedFreight = []promotion.FreightRequest{{
				Origin: "Warehouse/app", SourceStages: []string{"source"},
			}}
			if test.sourceFreight != nil {
				source.Freight.Items = []promotion.FreightItem{*test.sourceFreight}
			}
			if test.targetFreight != nil {
				target.Freight.Items = []promotion.FreightItem{*test.targetFreight}
			}
			stages := []promotion.Stage{target}
			if test.resolved {
				stages = append(stages, source)
			}
			stale := map[uuid.UUID]bool{source.ID: test.sourceStale, target.ID: test.targetStale}

			got := freightAlignments(stages, stale)[target.ID]
			if len(got) != 1 || got[0].Status != test.want {
				t.Fatalf("alignment = %#v, want status %q", got, test.want)
			}
			if test.resolved && (got[0].SourceStageID == nil || *got[0].SourceStageID != source.ID) {
				t.Errorf("resolved source Stage = %#v", got[0].SourceStageID)
			}
			if !test.resolved && got[0].SourceStageID != nil {
				t.Errorf("unresolved source Stage ID = %s", *got[0].SourceStageID)
			}
			if got[0].Origin != "Warehouse/app" || got[0].TargetStageID != target.ID {
				t.Errorf("comparison identity = %#v", got[0])
			}
			if test.want == FreightAlignmentAligned && got[0].Reason != "" {
				t.Errorf("aligned reason = %q", got[0].Reason)
			}
			if test.want != FreightAlignmentAligned && got[0].Reason == "" {
				t.Error("non-aligned comparison has no reason")
			}
		})
	}
}

func TestFreightAlignmentsKeepMultipleSourcesIndependentAndIgnoreDirectRequests(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	first := projectTestStage("prod", "colo", "kargo-core", "first", now)
	first.Freight.Items = []promotion.FreightItem{*freightAlignmentTestItem("freight-1")}
	second := projectTestStage("prod", "colo", "kargo-core", "second", now)
	second.Freight.Items = []promotion.FreightItem{*freightAlignmentTestItem("freight-2")}
	target := projectTestStage("prod", "colo", "kargo-core", "target", now)
	target.Freight.Items = []promotion.FreightItem{*freightAlignmentTestItem("freight-1")}
	target.RequestedFreight = []promotion.FreightRequest{
		{
			Origin: "Warehouse/app", SourceStages: []string{"first", "second"},
			AvailabilityStrategy: promotion.FreightAvailabilityAll,
		},
		{Origin: "Warehouse/config", Direct: true},
	}

	got := freightAlignments([]promotion.Stage{target, second, first}, map[uuid.UUID]bool{})[target.ID]
	if len(got) != 2 {
		t.Fatalf("alignments = %#v", got)
	}
	if got[0].SourceStageName != "first" || got[0].Status != FreightAlignmentAligned ||
		got[1].SourceStageName != "second" || got[1].Status != FreightAlignmentDifferent {
		t.Errorf("independent source comparisons = %#v", got)
	}
}

func freightAlignmentTestItem(name string) *promotion.FreightItem {
	return &promotion.FreightItem{
		Origin: "Warehouse/app", Name: name,
		Artifacts: []promotion.FreightArtifact{{Kind: "image", Version: "v1"}},
	}
}
