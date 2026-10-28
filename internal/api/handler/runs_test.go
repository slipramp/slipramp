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

func TestAPIExecutionImagesIncludesRecordedProvenance(t *testing.T) {
	t.Parallel()

	action := "registry.example.com/tools@sha256:action"
	checkout := "registry.example.com/checkout@sha256:checkout"
	harness := "registry.example.com/harness@sha256:harness"
	response := apiExecutionImages(run.ExecutionImages{
		Action:   &action,
		Checkout: &checkout,
		Harness:  &harness,
	})
	if response == nil || response.Action == nil || *response.Action != action ||
		response.Checkout == nil || *response.Checkout != checkout ||
		response.Harness == nil || *response.Harness != harness {
		t.Errorf("API execution images = %#v", response)
	}
	if empty := apiExecutionImages(run.ExecutionImages{}); empty != nil {
		t.Errorf("empty API execution images = %#v, want nil", empty)
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
				Title: "Apply approved plan",
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
		consumers["plan"][0].ActionTitle == nil ||
		*consumers["plan"][0].ActionTitle != "Apply approved plan" ||
		string(consumers["plan"][1].Action) != "deploy" {
		t.Errorf("plan consumers = %#v", consumers["plan"])
	}
}

func TestRunInputResponseUsesFrozenProducerDescription(t *testing.T) {
	t.Parallel()

	artifactID := uuid.New()
	snapshot := run.Snapshot{
		Action: catalog.EffectiveAction{
			Inputs: map[string]catalog.Input{
				"plan": {Artifact: catalog.ArtifactReference{FromAction: "preview"}},
			},
		},
		Target: catalog.Target{Spec: catalog.TargetSpec{Actions: map[string]catalog.Action{
			"preview": {
				Artifacts: map[string]catalog.Artifact{
					"plan": {Description: "Binary execution plan."},
				},
			},
		}}},
		Inputs: map[string]run.ArtifactInput{
			"plan": {ArtifactID: artifactID, Name: "plan"},
		},
	}

	inputs := runInputResponse(snapshot, nil)
	if inputs["plan"].ArtifactID != artifactID || inputs["plan"].Description == nil ||
		*inputs["plan"].Description != "Binary execution plan." {
		t.Errorf("runInputResponse() = %#v", inputs)
	}
}
