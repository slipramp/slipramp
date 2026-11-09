package promotion

import (
	"strings"
	"testing"
	"time"
)

func TestIntegrationSetValidate(t *testing.T) {
	t.Parallel()

	integrations := IntegrationSet{
		"prod": {
			ClusterName: "prod",
			UIURL:       "https://kargo.example.com/console/",
			ArgoCDUIURLs: map[string]string{
				"homelab": "https://argocd.homelab.example.com",
				"prod":    "https://argocd.example.com/console/",
			},
			Repositories: []string{"kcloud-ops"},
			Preparation: PreparationConfig{
				Repository: "kcloud-ops",
				Target:     "kargo-promotions",
				Action:     "prepare-review",
			},
			MaximumStageObservationAge: 5 * time.Minute,
			PendingReviewRetention:     8 * 24 * time.Hour,
		},
	}

	if err := integrations.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	integration := integrations["prod"]
	integration.Preparation.Repository = "other"
	integrations["prod"] = integration
	if err := integrations.Validate(); err == nil {
		t.Fatal("Validate() error = nil for preparation repository outside allowlist")
	}

	integration.Preparation.Repository = "kcloud-ops"
	integration.UIURL = "ftp://kargo.example.com"
	integrations["prod"] = integration
	if err := integrations.Validate(); err == nil {
		t.Fatal("Validate() error = nil for non-HTTP Kargo UI URL")
	}

	integration.UIURL = "https://kargo.example.com"
	integration.ArgoCDUIURLs["prod"] = "ftp://argocd.example.com"
	integrations["prod"] = integration
	if err := integrations.Validate(); err == nil {
		t.Fatal("Validate() error = nil for non-HTTP Argo CD UI URL")
	}
	integration.ArgoCDUIURLs["prod"] = "https://argocd.example.com"
	integration.CandidateBranchCleanup = &CandidateBranchCleanupConfig{
		Retention:   0,
		Credentials: map[string]string{"kcloud-ops": "github-write"},
	}
	integrations["prod"] = integration
	if err := integrations.Validate(); err == nil {
		t.Fatal("Validate() error = nil for zero candidate branch retention")
	}

	integration.CandidateBranchCleanup.Retention = 7 * 24 * time.Hour
	integration.CandidateBranchCleanup.Credentials = map[string]string{"other": "github-write"}
	integrations["prod"] = integration
	if err := integrations.Validate(); err == nil {
		t.Fatal("Validate() error = nil for candidate branch repository outside allowlist")
	}
}

func TestIntegrationSetBuildsKargoLinks(t *testing.T) {
	t.Parallel()

	integrations := IntegrationSet{
		"prod": {UIURL: "https://kargo.example.com/console/"},
	}
	projectURL := integrations.KargoProjectURL("prod", "kargo-core")
	if projectURL == nil || *projectURL != "https://kargo.example.com/console/project/kargo-core" {
		t.Errorf("KargoProjectURL() = %v", projectURL)
	}
	stageURL := integrations.KargoStageURL("prod", "kargo-core", "external-dns.prod")
	if stageURL == nil || *stageURL != "https://kargo.example.com/console/project/kargo-core/stage/external-dns.prod" {
		t.Errorf("KargoStageURL() = %v", stageURL)
	}
	promotionURL := integrations.KargoPromotionURL("prod", "kargo-core", "promotion-01")
	if promotionURL == nil || *promotionURL != "https://kargo.example.com/console/project/kargo-core/promotion/promotion-01" {
		t.Errorf("KargoPromotionURL() = %v", promotionURL)
	}
	if integrations.KargoStageURL("missing", "kargo-core", "prod") != nil {
		t.Error("KargoStageURL() returned a URL for an unknown integration")
	}
	if integrations.KargoProjectURL("missing", "kargo-core") != nil {
		t.Error("KargoProjectURL() returned a URL for an unknown integration")
	}
}

func TestIntegrationSetBuildsArgoCDApplicationLinks(t *testing.T) {
	t.Parallel()

	integrations := IntegrationSet{
		"prod": {
			ArgoCDUIURLs: map[string]string{
				"prod": "https://argocd.example.com/console/",
			},
		},
	}
	applicationURL := integrations.ArgoCDApplicationURL("prod", "prod", "argocd", "external-dns")
	if applicationURL == nil ||
		*applicationURL != "https://argocd.example.com/console/applications/argocd/external-dns" {
		t.Errorf("ArgoCDApplicationURL() = %v", applicationURL)
	}
	applicationURL = integrations.ArgoCDApplicationURL("prod", "prod", "", "external-dns")
	if applicationURL == nil ||
		*applicationURL != "https://argocd.example.com/console/applications/external-dns" {
		t.Errorf("ArgoCDApplicationURL() without namespace = %v", applicationURL)
	}
	if integrations.ArgoCDApplicationURL("prod", "homelab", "argocd", "external-dns") != nil {
		t.Error("ArgoCDApplicationURL() returned a URL for an unconfigured environment")
	}
	if integrations.ArgoCDApplicationURL("missing", "prod", "argocd", "external-dns") != nil {
		t.Error("ArgoCDApplicationURL() returned a URL for an unknown integration")
	}
	if integrations.ArgoCDApplicationURL("prod", "prod", "argocd", "") != nil {
		t.Error("ArgoCDApplicationURL() returned a URL without an Application")
	}
}

func TestValidateObserverConfigs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configs   []ObserverConfig
		wantError string
	}{
		{
			name: "distinct namespace owners",
			configs: []ObserverConfig{
				{Name: "internal", StageNamespaces: []string{"kargo-internal", "kargo-platform"}},
				{Name: "customer", StageNamespaces: []string{"kargo-customer"}},
			},
		},
		{
			name: "duplicate integration",
			configs: []ObserverConfig{
				{Name: "prod", StageNamespaces: []string{"kargo-core"}},
				{Name: "prod", StageNamespaces: []string{"kargo-platform"}},
			},
			wantError: `kargo integration "prod" is configured more than once`,
		},
		{
			name:      "integration without namespaces",
			configs:   []ObserverConfig{{Name: "prod"}},
			wantError: `kargo integration "prod" requires at least one Stage namespace`,
		},
		{
			name: "duplicate namespace in one integration",
			configs: []ObserverConfig{
				{Name: "prod", StageNamespaces: []string{"kargo-core", "kargo-core"}},
			},
			wantError: `kargo Stage namespace "kargo-core" is configured more than once for integration "prod"`,
		},
		{
			name: "shared namespace",
			configs: []ObserverConfig{
				{Name: "internal", StageNamespaces: []string{"shared-kargo"}},
				{Name: "customer", StageNamespaces: []string{"shared-kargo"}},
			},
			wantError: `kargo Stage namespace "shared-kargo" is configured for both integrations "customer" and "internal"`,
		},
		{
			name: "shared namespace in reverse order",
			configs: []ObserverConfig{
				{Name: "customer", StageNamespaces: []string{"shared-kargo"}},
				{Name: "internal", StageNamespaces: []string{"shared-kargo"}},
			},
			wantError: `kargo Stage namespace "shared-kargo" is configured for both integrations "customer" and "internal"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateObserverConfigs(test.configs)
			if test.wantError == "" && err != nil {
				t.Fatalf("ValidateObserverConfigs() error = %v", err)
			}
			if test.wantError != "" && (err == nil || err.Error() != test.wantError) {
				t.Fatalf("ValidateObserverConfigs() error = %v, want %q", err, test.wantError)
			}
		})
	}

	for _, test := range []struct {
		name        string
		config      ObserverConfig
		wantSubject string
	}{
		{
			name:        "invalid integration name",
			config:      ObserverConfig{Name: "Internal", StageNamespaces: []string{"kargo-internal"}},
			wantSubject: "Kargo integration name",
		},
		{
			name:        "invalid namespace name",
			config:      ObserverConfig{Name: "internal", StageNamespaces: []string{"Shared_Kargo"}},
			wantSubject: "Kargo Stage namespace",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateObserverConfigs([]ObserverConfig{test.config})
			if err == nil || !strings.Contains(err.Error(), test.wantSubject) {
				t.Fatalf("ValidateObserverConfigs() error = %v, want subject %q", err, test.wantSubject)
			}
		})
	}
}
