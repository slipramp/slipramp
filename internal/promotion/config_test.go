package promotion

import (
	"testing"
	"time"
)

func TestIntegrationSetValidate(t *testing.T) {
	t.Parallel()

	integrations := IntegrationSet{
		"prod": {
			ClusterName:      "prod",
			UIURL:            "https://kargo.example.com/console/",
			Repositories:     []string{"kcloud-ops"},
			EnvironmentOrder: []string{"homelab", "prod"},
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
	integration.EnvironmentOrder = nil
	integrations["prod"] = integration
	if err := integrations.Validate(); err == nil {
		t.Fatal("Validate() error = nil for missing environment order")
	}

	integration.EnvironmentOrder = []string{"prod", "prod"}
	integrations["prod"] = integration
	if err := integrations.Validate(); err == nil {
		t.Fatal("Validate() error = nil for duplicate environment")
	}

	integration.EnvironmentOrder = []string{"homelab", "prod"}
	integration.UIURL = "ftp://kargo.example.com"
	integrations["prod"] = integration
	if err := integrations.Validate(); err == nil {
		t.Fatal("Validate() error = nil for non-HTTP Kargo UI URL")
	}

	integration.UIURL = "https://kargo.example.com"
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

func TestValidateObserverConfigs(t *testing.T) {
	t.Parallel()

	configs := []ObserverConfig{{
		Name:            "prod",
		StageNamespaces: []string{"kargo-core", "kargo-platform"},
	}}
	if err := ValidateObserverConfigs(configs); err != nil {
		t.Fatalf("ValidateObserverConfigs() error = %v", err)
	}

	configs = append(configs, configs[0])
	if err := ValidateObserverConfigs(configs); err == nil {
		t.Fatal("ValidateObserverConfigs() error = nil for duplicate integration")
	}
}
