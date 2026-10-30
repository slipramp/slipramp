package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestLoadServerKargoIntegrationsFile(t *testing.T) {
	t.Parallel()

	path := writeKargoIntegrationsFile(t, `
apiVersion: slipramp.com/v1alpha1
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
    maximumStageObservationAge: 5m
    pendingReviewRetention: 192h
`)
	environment := requiredServerEnvironment()
	environment[serverControllerEnrollmentTokensEnvironment] = `{"colo":"enrollment-token"}`
	environment[serverKargoIntegrationsFileEnvironment] = path

	cfg, err := config.LoadServer(mapEnvironment(environment))
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}
	if _, ok := cfg.KargoIntegrations.Lookup("prod"); !ok {
		t.Fatal("KargoIntegrations.Lookup(prod) found = false, want true")
	}
}

func TestLoadServerRejectsInvalidKargoIntegrationsFile(t *testing.T) {
	t.Parallel()

	path := writeKargoIntegrationsFile(t, `
apiVersion: slipramp.com/v1alpha1
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
    maximumStageObservationAge: 5m
    pendingReviewRetention: 192h
    unknown: value
`)
	environment := requiredServerEnvironment()
	environment[serverKargoIntegrationsFileEnvironment] = path

	_, err := config.LoadServer(mapEnvironment(environment))
	testcheck.ErrorContains(t, err, serverKargoIntegrationsFileEnvironment)
	testcheck.ErrorContains(t, err, "field unknown not found")
}

func writeKargoIntegrationsFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "kargo-integrations.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write Kargo integrations file: %v", err)
	}

	return path
}
