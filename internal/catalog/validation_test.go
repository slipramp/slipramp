package catalog_test

import (
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/testcheck"
	"go.yaml.in/yaml/v3"
)

type validationTest struct {
	name      string
	configure func(*catalog.Catalog)
	wantError string
}

func TestValidateRejectsIncompleteActions(t *testing.T) {
	t.Parallel()

	tests := []validationTest{
		{
			name: "runtime without image",
			configure: func(input *catalog.Catalog) {
				input.Runtimes[0].Spec.Image = ""
			},
			wantError: "runtime spec.image is required",
		},
		{
			name: "unknown runtime",
			configure: func(input *catalog.Catalog) {
				input.Targets[0].Spec.Runtime = "missing"
			},
			wantError: `runtime "missing" is not defined`,
		},
		{
			name: "unknown placement",
			configure: func(input *catalog.Catalog) {
				input.Targets[0].Spec.Placement = "missing"
			},
			wantError: `placement "missing" is not defined`,
		},
		{
			name: "missing command",
			configure: func(input *catalog.Catalog) {
				action := input.Targets[0].Spec.Actions["preview"]
				action.Command = nil
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: `action "preview" command is required`,
		},
		{
			name: "unsupported trigger",
			configure: func(input *catalog.Catalog) {
				action := input.Targets[0].Spec.Actions["preview"]
				action.Triggers = []yaml.Node{{Kind: yaml.MappingNode}}
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: "declares triggers, which are not supported",
		},
	}

	runValidationTests(t, tests)
}

func runValidationTests(t *testing.T, tests []validationTest) {
	t.Helper()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := validCatalog()
			test.configure(&input)
			testcheck.ErrorContains(
				t,
				catalog.Validate(input, testPlacements(t)),
				test.wantError,
			)
		})
	}
}

func testPlacements(t *testing.T) placement.Set {
	t.Helper()

	credentials := map[string]placement.CredentialSource{
		"credential-source": {
			SecretRef: placement.SecretReference{Name: "credential-source"},
		},
		"terraform-backend": {
			SecretRef: placement.SecretReference{Name: "terraform-backend"},
		},
		"terraform-plugins": {
			SecretRef: placement.SecretReference{Name: "terraform-plugins"},
		},
	}
	configured, err := placement.NewSet(map[string]placement.Config{
		"infrastructure-admin": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-runs",
			Credentials:  credentials,
		},
		"preview-cluster": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-runs",
			Credentials:  credentials,
		},
	})
	if err != nil {
		t.Fatalf("placement.NewSet() error = %v", err)
	}

	return configured
}

func validCatalog() catalog.Catalog {
	source := catalog.Source{
		Path:     ".slipramp/catalog.yaml",
		Document: 1,
	}

	return catalog.Catalog{
		Runtimes: []catalog.Runtime{{
			Metadata: catalog.Metadata{Name: "terraform"},
			Spec: catalog.RuntimeSpec{
				Image: "hashicorp/terraform:1.14.3",
			},
			Source: source,
		}},
		Targets: []catalog.Target{{
			Metadata: catalog.Metadata{Name: "network"},
			Spec: catalog.TargetSpec{
				Runtime:   "terraform",
				Placement: "infrastructure-admin",
				Actions: map[string]catalog.Action{
					"preview": {
						Command: []string{"terraform", "plan"},
					},
				},
			},
			Source: source,
		}},
	}
}
