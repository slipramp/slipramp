package catalog_test

import (
	"math"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestValidateGate(t *testing.T) {
	t.Parallel()

	tests := []validationTest{
		{
			name: "invalid Gate name",
			configure: func(input *catalog.Catalog) {
				input.Gates[0].Metadata.Name = "NOT_VALID"
			},
			wantError: "Gate metadata.name",
		},
		{
			name: "invalid source name",
			configure: func(input *catalog.Catalog) {
				input.Gates[0].Spec.Prometheus.Source = "NOT_VALID"
			},
			wantError: "Gate Prometheus source",
		},
		{
			name: "empty query",
			configure: func(input *catalog.Catalog) {
				input.Gates[0].Spec.Prometheus.Query = ""
			},
			wantError: "Gate Prometheus query is required",
		},
		{
			name: "oversized query",
			configure: func(input *catalog.Catalog) {
				input.Gates[0].Spec.Prometheus.Query = strings.Repeat("x", gate.MaximumQueryLength+1)
			},
			wantError: "Gate Prometheus query must not exceed",
		},
		{
			name: "unsupported operator",
			configure: func(input *catalog.Catalog) {
				input.Gates[0].Spec.Prometheus.Condition.Operator = "approximately"
			},
			wantError: `Gate condition operator "approximately" is not supported`,
		},
		{
			name: "non-finite threshold",
			configure: func(input *catalog.Catalog) {
				input.Gates[0].Spec.Prometheus.Condition.Value = math.Inf(1)
			},
			wantError: "Gate condition value must be finite",
		},
		{
			name: "missing Action Gate",
			configure: func(input *catalog.Catalog) {
				action := input.Targets[0].Spec.Actions["preview"]
				action.Gates = []string{"missing"}
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: `references Gate "missing" that is not defined`,
		},
		{
			name: "duplicate Action Gate",
			configure: func(input *catalog.Catalog) {
				action := input.Targets[0].Spec.Actions["preview"]
				action.Gates = []string{"network-idle", "network-idle"}
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: `action "preview" Gate "network-idle" is duplicated`,
		},
	}

	runValidationTests(t, tests)
}

func TestValidateGateDeploymentRequiresConfiguredSource(t *testing.T) {
	t.Parallel()

	input := validCatalog()
	diagnostics := catalog.ValidateGateDeployment(input, gate.SourceSet{})
	if len(diagnostics) != 1 || diagnostics[0].Code != "catalog.gate.source-not-found" ||
		diagnostics[0].Location.FieldPath != "spec.prometheus.source" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	testcheck.ErrorContains(t, diagnosticsAsError(diagnostics), `source "homelab" is not configured`)

	configured := gate.SourceSet{"homelab": {}}
	if diagnostics := catalog.ValidateGateDeployment(input, configured); len(diagnostics) != 0 {
		t.Errorf("configured diagnostics = %#v", diagnostics)
	}
}
