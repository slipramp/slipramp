package promotion

import (
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestDecodeIntegrationFile(t *testing.T) {
	t.Parallel()

	integrations, err := DecodeIntegrationFile(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
integrations:
  prod:
    cluster: colo
    uiUrl: https://kargo.example.com
    argoCdUiUrls:
      homelab: https://argocd.homelab.example.com
      prod: https://argocd.example.com
    repositories: [kcloud-ops]
    environmentOrder: [homelab, prod]
    preparation:
      repository: kcloud-ops
      target: kargo-promotions
      action: prepare-review
    candidateBranchCleanup:
      retention: 168h
      credentials:
        kcloud-ops: github-write
    maximumStageObservationAge: 5m
    pendingReviewRetention: 192h
`))
	if err != nil {
		t.Fatalf("DecodeIntegrationFile() error = %v", err)
	}

	integration, ok := integrations.Lookup("prod")
	if !ok {
		t.Fatal("Lookup(prod) found = false, want true")
	}
	if integration.ClusterName != "colo" || integration.UIURL != "https://kargo.example.com" {
		t.Errorf("integration = %#v", integration)
	}
	if integration.ArgoCDUIURLs["prod"] != "https://argocd.example.com" ||
		integration.MaximumStageObservationAge != 5*time.Minute ||
		integration.PendingReviewRetention != 8*24*time.Hour {
		t.Errorf("integration = %#v", integration)
	}
	if integration.CandidateBranchCleanup == nil ||
		integration.CandidateBranchCleanup.Retention != 7*24*time.Hour ||
		integration.CandidateBranchCleanup.Credentials["kcloud-ops"] != "github-write" {
		t.Errorf("CandidateBranchCleanup = %#v", integration.CandidateBranchCleanup)
	}
}

func TestDecodeIntegrationFileAllowsNoIntegrations(t *testing.T) {
	t.Parallel()

	integrations, err := DecodeIntegrationFile(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
integrations: {}
`))
	if err != nil {
		t.Fatalf("DecodeIntegrationFile() error = %v", err)
	}
	if len(integrations) != 0 {
		t.Errorf("len(integrations) = %d, want 0", len(integrations))
	}
}

func TestDecodeIntegrationFileRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		contents  string
		wantError string
	}{
		{
			name: "unsupported API version",
			contents: `apiVersion: slipramp.com/v2
kind: KargoIntegrationConfig
integrations: {}
`,
			wantError: "apiVersion must be \"slipramp.com/v1alpha1\"",
		},
		{
			name: "unsupported kind",
			contents: `apiVersion: slipramp.com/v1alpha1
kind: Integrations
integrations: {}
`,
			wantError: "kind must be \"KargoIntegrationConfig\"",
		},
		{
			name: "unknown field",
			contents: `apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
integrations: {}
unknown: value
`,
			wantError: "field unknown not found",
		},
		{
			name: "duplicate key",
			contents: `apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
integrations: {}
integrations: {}
`,
			wantError: "mapping key \"integrations\" already defined",
		},
		{
			name: "multiple documents",
			contents: `apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
integrations: {}
---
apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
integrations: {}
`,
			wantError: "exactly one document",
		},
		{
			name: "missing integrations",
			contents: `apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
`,
			wantError: "must define integrations",
		},
		{
			name: "invalid duration",
			contents: `apiVersion: slipramp.com/v1alpha1
kind: KargoIntegrationConfig
integrations:
  prod:
    cluster: colo
    repositories: [kcloud-ops]
    environmentOrder: [prod]
    preparation:
      repository: kcloud-ops
      target: kargo-promotions
      action: prepare-review
    maximumStageObservationAge: soon
    pendingReviewRetention: 192h
`,
			wantError: "maximum Stage observation age",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeIntegrationFile(strings.NewReader(test.contents))
			testcheck.ErrorContains(t, err, test.wantError)
		})
	}
}
