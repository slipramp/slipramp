package catalog_test

import (
	"testing"
	"testing/fstest"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

func TestProcessAggregatesAndLocatesDiagnostics(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		".slipramp/runtime.yaml": {Data: []byte(`
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: tools
spec:
  image: ""
`)},
		".slipramp/target.yaml": {Data: []byte(`
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: operations
spec:
  runtime: tools
  placement: future-placement
  actions:
    apply:
      command: [apply]
      inputs:
        plan:
          artifact:
            fromAction: preview
          mountPath: /inputs/plan
`)},
	}

	result, err := catalog.Process(files, catalog.Options{})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if !result.Diagnostics.HasErrors() {
		t.Fatal("Process() diagnostics contain no errors")
	}

	image := findDiagnostic(t, result.Diagnostics, "catalog.runtime.image-invalid")
	if image.Location.Path != ".slipramp/runtime.yaml" ||
		image.Location.Document != 1 ||
		image.Location.Line != 7 ||
		image.Location.FieldPath != "spec.image" {
		t.Errorf("image diagnostic location = %#v", image.Location)
	}

	artifact := findDiagnostic(t, result.Diagnostics, "catalog.artifact.source-action-missing")
	if artifact.Location.Path != ".slipramp/target.yaml" ||
		artifact.Location.Line != 15 ||
		artifact.Location.Column != 25 ||
		artifact.Location.FieldPath != "spec.actions.apply.inputs.plan.artifact.fromAction" {
		t.Errorf("artifact diagnostic location = %#v", artifact.Location)
	}

	for _, item := range result.Diagnostics {
		if item.Code == "catalog.placement.not-found" {
			t.Fatal("portable validation rejected an unknown Placement")
		}
	}
}

func TestProcessDeploymentValidationIsExplicit(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		".slipramp/catalog.yaml": {Data: []byte(`
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: tools
spec:
  image: example.com/tools:1
---
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: operations
spec:
  runtime: tools
  placement: future-placement
  actions:
    inspect:
      command: [inspect]
`)},
	}

	portable, err := catalog.Process(files, catalog.Options{})
	if err != nil {
		t.Fatalf("portable Process() error = %v", err)
	}
	if portable.Diagnostics.HasErrors() {
		t.Fatalf("portable diagnostics = %s", portable.Diagnostics.ErrorSummary())
	}

	empty, err := placement.NewSet(map[string]placement.Config{})
	if err != nil {
		t.Fatalf("placement.NewSet() error = %v", err)
	}
	deployed, err := catalog.Process(files, catalog.Options{Placements: &empty})
	if err != nil {
		t.Fatalf("deployment Process() error = %v", err)
	}
	missing := findDiagnostic(t, deployed.Diagnostics, "catalog.placement.not-found")
	if missing.Location.FieldPath != "spec.placement" {
		t.Errorf("missing Placement field path = %q", missing.Location.FieldPath)
	}
}

func TestDecodeLocatesStructuralDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		contents  string
		code      string
		line      int
		column    int
		fieldPath string
	}{
		{
			name: "unknown field",
			contents: `apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: tools
spec:
  image: example.com/tools:1
  typo: rejected
`,
			code:      "catalog.yaml.unknown-field",
			line:      7,
			column:    3,
			fieldPath: "spec.typo",
		},
		{
			name: "duplicate key",
			contents: `apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: tools
spec:
  image: example.com/tools:1
  image: example.com/tools:2
`,
			code:      "catalog.yaml.duplicate-key",
			line:      7,
			column:    3,
			fieldPath: "spec.image",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			files := fstest.MapFS{
				".slipramp/catalog.yaml": {Data: []byte(test.contents)},
			}
			_, diagnostics, err := catalog.Decode(files)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			item := findDiagnostic(t, diagnostics, test.code)
			if item.Location.Line != test.line ||
				item.Location.Column != test.column ||
				item.Location.FieldPath != test.fieldPath {
				t.Errorf("diagnostic location = %#v", item.Location)
			}
		})
	}
}

func TestPortableValidationWarnsForSubsumedWatchPattern(t *testing.T) {
	t.Parallel()

	input := validCatalog()
	input.Targets[0].Spec.Watch = []string{
		"infrastructure/network/main.tf",
		"infrastructure/**",
	}

	diagnostics := catalog.ValidatePortable(input)
	item := findDiagnostic(t, diagnostics, "catalog.watch.subsumed")
	if item.Severity != catalog.SeverityWarning || len(item.Related) != 1 {
		t.Errorf("subsumed diagnostic = %#v", item)
	}
}

func TestPortableValidationAggregatesLeafDiagnostics(t *testing.T) {
	t.Parallel()

	input := validCatalog()
	input.Runtimes[0].Spec.Environment = map[string]string{"TAKEN": "value"}
	action := input.Targets[0].Spec.Actions["preview"]
	action.Parameters = map[string]catalog.Parameter{
		"bad-type": {
			Type:        "integer",
			Environment: "COUNT",
		},
		"collision": {
			Type:        catalog.ParameterTypeString,
			Environment: "TAKEN",
		},
	}
	action.Artifacts = map[string]catalog.Artifact{
		"plan": {
			Path:      "relative-plan",
			MediaType: "not a media type",
		},
	}
	action.Inputs = map[string]catalog.Input{
		"plan": {
			Artifact: catalog.ArtifactReference{
				FromAction:    "preview",
				Compatibility: "latest",
			},
			MountPath: "relative-input",
		},
	}
	action.Triggers = []catalog.Trigger{
		{Name: "Not Valid"},
		{Name: "Not Valid"},
	}
	input.Targets[0].Spec.Actions["preview"] = action

	diagnostics := catalog.ValidatePortable(input)
	want := map[string]string{
		"catalog.parameter.type-invalid":         "spec.actions.preview.parameters.bad-type.type",
		"catalog.parameter.environment-conflict": "spec.actions.preview.parameters.collision.environment",
		"catalog.artifact.path-invalid":          "spec.actions.preview.artifacts.plan.path",
		"catalog.artifact.media-type-invalid":    "spec.actions.preview.artifacts.plan.mediaType",
		"catalog.input.compatibility-invalid":    "spec.actions.preview.inputs.plan.artifact.compatibility",
		"catalog.input.mount-path-invalid":       "spec.actions.preview.inputs.plan.mountPath",
		"catalog.trigger.name-duplicate":         "spec.actions.preview.triggers.1.name",
	}
	for code, fieldPath := range want {
		item := findDiagnostic(t, diagnostics, code)
		if item.Location.FieldPath != fieldPath {
			t.Errorf("diagnostic %q field path = %q, want %q", code, item.Location.FieldPath, fieldPath)
		}
	}
	if countDiagnostics(diagnostics, "catalog.trigger.name-invalid") != 2 {
		t.Errorf("trigger name diagnostics = %#v", diagnostics)
	}
	if countDiagnostics(diagnostics, "catalog.trigger.source-required") != 2 {
		t.Errorf("trigger source diagnostics = %#v", diagnostics)
	}
	for _, obsolete := range []string{
		"catalog.parameter.invalid",
		"catalog.artifact.invalid",
		"catalog.input.invalid",
		"catalog.trigger.invalid",
	} {
		if countDiagnostics(diagnostics, obsolete) != 0 {
			t.Errorf("obsolete diagnostic %q remains in %#v", obsolete, diagnostics)
		}
	}
}

func countDiagnostics(diagnostics catalog.Diagnostics, code string) int {
	count := 0
	for _, item := range diagnostics {
		if item.Code == code {
			count++
		}
	}

	return count
}

func findDiagnostic(t *testing.T, diagnostics catalog.Diagnostics, code string) catalog.Diagnostic {
	t.Helper()

	for _, item := range diagnostics {
		if item.Code == code {
			return item
		}
	}

	t.Fatalf("diagnostic %q not found in %#v", code, diagnostics)

	return catalog.Diagnostic{}
}
