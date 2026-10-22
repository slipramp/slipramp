package promotion

import (
	"testing"
	"time"
)

func TestIntegrationSetValidate(t *testing.T) {
	t.Parallel()

	integrations := IntegrationSet{
		"prod": {
			ClusterName:  "prod",
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
