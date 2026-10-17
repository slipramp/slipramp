package catalog_test

import (
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
)

func TestValidateExecutionDefaultsAndArtifacts(t *testing.T) {
	t.Parallel()

	tests := []validationTest{
		{
			name: "complete",
			configure: func(input *catalog.Catalog) {
				addPreviewAndApply(input)
				input.Targets[0].Spec.Path = "infrastructure/network"
				input.Targets[0].Spec.Watch = []string{"infrastructure/network/**"}
				input.Targets[0].Spec.Timeout = "30m"
				input.Targets[0].Spec.Resources = catalog.ResourceRequirements{
					Requests: map[string]string{"cpu": "100m", "memory": "128Mi"},
					Limits:   map[string]string{"memory": "1Gi"},
				}
			},
		},
		{
			name: "invalid target path",
			configure: func(input *catalog.Catalog) {
				input.Targets[0].Spec.Path = "../outside"
			},
			wantError: "repository-relative path",
		},
		{
			name: "invalid watch pattern",
			configure: func(input *catalog.Catalog) {
				input.Targets[0].Spec.Watch = []string{"infrastructure/["}
			},
			wantError: "syntax error in pattern",
		},
		{
			name: "invalid timeout",
			configure: func(input *catalog.Catalog) {
				input.Targets[0].Spec.Timeout = "forever"
			},
			wantError: "invalid duration",
		},
		{
			name: "request exceeds inherited limit",
			configure: func(input *catalog.Catalog) {
				input.Targets[0].Spec.Resources.Limits = map[string]string{"memory": "1Gi"}
				action := input.Targets[0].Spec.Actions["preview"]
				action.Resources.Requests = map[string]string{"memory": "2Gi"}
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: "must not exceed its limit",
		},
		{
			name: "invalid intent",
			configure: func(input *catalog.Catalog) {
				action := input.Targets[0].Spec.Actions["preview"]
				action.Intent = "workflow"
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: "intent must be",
		},
		{
			name: "invalid artifact path",
			configure: func(input *catalog.Catalog) {
				action := input.Targets[0].Spec.Actions["preview"]
				action.Artifacts = map[string]catalog.Artifact{"plan": {Path: "plan.tfplan"}}
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: "clean absolute path",
		},
		{
			name: "invalid media type",
			configure: func(input *catalog.Catalog) {
				action := input.Targets[0].Spec.Actions["preview"]
				action.Artifacts = map[string]catalog.Artifact{
					"plan": {Path: "/artifacts/plan", MediaType: "not a media type"},
				}
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: "mediaType",
		},
		{
			name: "undefined source action",
			configure: func(input *catalog.Catalog) {
				addPreviewAndApply(input)
				action := input.Targets[0].Spec.Actions["apply"]
				plan := action.Inputs["plan"]
				plan.Artifact.FromAction = "missing"
				action.Inputs["plan"] = plan
				input.Targets[0].Spec.Actions["apply"] = action
			},
			wantError: "references undefined action",
		},
		{
			name: "missing source artifact",
			configure: func(input *catalog.Catalog) {
				addPreviewAndApply(input)
				preview := input.Targets[0].Spec.Actions["preview"]
				preview.Artifacts = nil
				input.Targets[0].Spec.Actions["preview"] = preview
			},
			wantError: "references missing artifact",
		},
		{
			name: "invalid compatibility",
			configure: func(input *catalog.Catalog) {
				addPreviewAndApply(input)
				action := input.Targets[0].Spec.Actions["apply"]
				plan := action.Inputs["plan"]
				plan.Artifact.Compatibility = "latest"
				action.Inputs["plan"] = plan
				input.Targets[0].Spec.Actions["apply"] = action
			},
			wantError: "compatibility must be",
		},
		{
			name: "input and artifact path collision",
			configure: func(input *catalog.Catalog) {
				addPreviewAndApply(input)
				action := input.Targets[0].Spec.Actions["apply"]
				action.Artifacts = map[string]catalog.Artifact{
					"result": {Path: "/slipramp/inputs/plan.tfplan"},
				}
				input.Targets[0].Spec.Actions["apply"] = action
			},
			wantError: "mountPath conflicts with artifact",
		},
	}

	runValidationTests(t, tests)
}

func addPreviewAndApply(input *catalog.Catalog) {
	preview := input.Targets[0].Spec.Actions["preview"]
	preview.Intent = "preview"
	preview.Artifacts = map[string]catalog.Artifact{
		"plan": {
			Path:      "/slipramp/artifacts/plan.tfplan",
			MediaType: "application/vnd.hashicorp.terraform.plan",
			Sensitive: true,
		},
	}
	input.Targets[0].Spec.Actions["preview"] = preview
	input.Targets[0].Spec.Actions["apply"] = catalog.Action{
		Intent:  "reconcile",
		Command: []string{"terraform", "apply", "/slipramp/inputs/plan.tfplan"},
		Inputs: map[string]catalog.Input{
			"plan": {
				Artifact: catalog.ArtifactReference{
					FromAction: "preview",
				},
				MountPath: "/slipramp/inputs/plan.tfplan",
			},
		},
	}
}
