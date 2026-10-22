package handler

import (
	"testing"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/run"
)

func TestAPIRunSourceIncludesPromotionBacklink(t *testing.T) {
	t.Parallel()

	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1312")
	response := apiRunSource(run.Source{
		Type: run.SourceTypePromotion,
		Promotion: &run.PromotionRunSource{
			PromotionID: promotionID,
		},
	})
	if response.Type != api.RunSourceTypePromotion || response.Promotion == nil ||
		response.Promotion.PromotionID != promotionID {
		t.Errorf("API Run source = %#v", response)
	}
}

func TestRunArtifactConsumersUsesFrozenDirectlyRunnableActions(t *testing.T) {
	t.Parallel()

	snapshot := run.Snapshot{
		Action: catalog.EffectiveAction{
			Name: "preview",
			Artifacts: map[string]catalog.Artifact{
				"plan": {Path: "/artifacts/plan.tfplan"},
			},
		},
		Target: catalog.Target{Spec: catalog.TargetSpec{Actions: map[string]catalog.Action{
			"apply": {
				Inputs: map[string]catalog.Input{
					"plan": {Artifact: catalog.ArtifactReference{FromAction: "preview"}},
				},
			},
			"apply-with-approval": {
				Parameters: map[string]catalog.Parameter{
					"ticket": {Type: catalog.ParameterTypeString, Required: true},
				},
				Inputs: map[string]catalog.Input{
					"plan": {Artifact: catalog.ArtifactReference{FromAction: "preview"}},
				},
			},
			"deploy": {
				Parameters: map[string]catalog.Parameter{
					"force": {Type: catalog.ParameterTypeBoolean},
				},
				Inputs: map[string]catalog.Input{
					"plan": {Artifact: catalog.ArtifactReference{FromAction: "preview"}},
				},
			},
			"combine": {
				Inputs: map[string]catalog.Input{
					"plan":  {Artifact: catalog.ArtifactReference{FromAction: "preview"}},
					"state": {Artifact: catalog.ArtifactReference{FromAction: "inspect"}},
				},
			},
		}}},
	}

	consumers := runArtifactConsumers(snapshot)
	if len(consumers) != 1 || len(consumers["plan"]) != 2 {
		t.Fatalf("runArtifactConsumers() = %#v", consumers)
	}
	if string(consumers["plan"][0].Action) != "apply" ||
		string(consumers["plan"][1].Action) != "deploy" {
		t.Errorf("plan consumers = %#v", consumers["plan"])
	}
}
