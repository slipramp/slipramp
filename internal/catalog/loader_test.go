package catalog_test

import (
	"testing"
	"testing/fstest"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		".slipramp/catalog.yaml": {Data: []byte(`
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: terraform
spec:
  image: hashicorp/terraform:1.14.3
  environment:
    TF_IN_AUTOMATION: "true"
  credentials:
    backend:
      inject:
        files:
          - key: config
            path: /var/run/terraform/config
            mode: 0400
---
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: network
  title: Home network
spec:
  path: infrastructure/network
  runtime: terraform
  placement: infrastructure-admin
  credentials:
    backend: terraform-backend
  actions:
    preview:
      intent: preview
      command: [terraform, plan]
      artifacts:
        plan:
          path: /slipramp/artifacts/plan.tfplan
          sensitive: true
`)},
		"deployment/values.yaml": {Data: []byte("not: a catalog resource\n")},
	}

	loaded, err := catalog.Load(files, testPlacements(t))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded.Runtimes) != 1 {
		t.Fatalf("len(Runtimes) = %d, want 1", len(loaded.Runtimes))
	}

	runtime := loaded.Runtimes[0]
	backendFile := runtime.Spec.Credentials["backend"].Inject.Files[0]
	if runtime.Metadata.Name != "terraform" ||
		runtime.Spec.Image != "hashicorp/terraform:1.14.3" ||
		runtime.Spec.Environment["TF_IN_AUTOMATION"] != "true" ||
		backendFile.Mode != 0o400 {
		t.Errorf("Runtime = %#v", runtime)
	}

	if len(loaded.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(loaded.Targets))
	}

	target := loaded.Targets[0]
	preview := target.Spec.Actions["preview"]
	plan := preview.Artifacts["plan"]
	if target.Metadata.Title != "Home network" ||
		target.Spec.Runtime != "terraform" ||
		len(preview.Command) != 2 ||
		plan.Path != "/slipramp/artifacts/plan.tfplan" ||
		!plan.Sensitive {
		t.Errorf("Target = %#v", target)
	}

	wantSource := catalog.Source{
		Path:     ".slipramp/catalog.yaml",
		Document: 2,
	}
	if target.Source != wantSource {
		t.Errorf("Target.Source = %#v, want %#v", target.Source, wantSource)
	}
}

func TestLoadRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		contents  string
		wantError string
	}{
		{
			name:      "malformed YAML",
			contents:  "apiVersion: [",
			wantError: ".slipramp/catalog.yaml document 1",
		},
		{
			name: "unsupported resource",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: Placement
metadata:
  name: infrastructure
`,
			wantError: `kind must be "Runtime" or "Target"`,
		},
		{
			name: "missing name",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata: {}
spec: {}
`,
			wantError: "metadata.name is required",
		},
		{
			name: "duplicate resource",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: terraform
spec: {}
---
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: terraform
spec: {}
`,
			wantError: `Runtime "terraform" is already defined`,
		},
		{
			name: "unknown field",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: terraform
spec:
  image: hashicorp/terraform:1.14.3
  typo: rejected
`,
			wantError: `field typo not found`,
		},
		{
			name: "duplicate field",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: terraform
spec:
  image: hashicorp/terraform:1.14.3
  image: hashicorp/terraform:1.14.4
`,
			wantError: `mapping key "image" already defined`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			files := fstest.MapFS{
				".slipramp/catalog.yaml": {
					Data: []byte(test.contents),
				},
			}

			_, err := catalog.Load(files, testPlacements(t))
			testcheck.ErrorContains(t, err, test.wantError)
		})
	}
}
