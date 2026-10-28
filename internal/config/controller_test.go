package config_test

import (
	"testing"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/testcheck"
)

const (
	controllerClusterEnvironment             = "SLIPRAMP_CONTROLLER_CLUSTER"
	controllerServerEnvironment              = "SLIPRAMP_CONTROLLER_SERVER_URL"
	controllerMetricsAddressEnvironment      = "SLIPRAMP_CONTROLLER_METRICS_ADDRESS"
	controllerMaximumActiveRunsEnvironment   = "SLIPRAMP_CONTROLLER_MAXIMUM_ACTIVE_RUNS"
	controllerEnrollmentTokenEnvironment     = "SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN"
	controllerTokenEnvironment               = "SLIPRAMP_CONTROLLER_TOKEN"
	controllerCheckoutImageEnvironment       = "SLIPRAMP_CONTROLLER_CHECKOUT_IMAGE"
	controllerHarnessImageEnvironment        = "SLIPRAMP_CONTROLLER_HARNESS_IMAGE"
	controllerGitCredentialsEnvironment      = "SLIPRAMP_CONTROLLER_GIT_CREDENTIALS"
	controllerExecutionNamespacesEnvironment = "SLIPRAMP_CONTROLLER_EXECUTION_NAMESPACES"
	controllerKargoIntegrationsEnvironment   = "SLIPRAMP_CONTROLLER_KARGO_INTEGRATIONS"
)

func TestLoadControllerFromEnvironment(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		controllerClusterEnvironment:             "colo",
		controllerServerEnvironment:              "https://slipramp.example.com",
		controllerMetricsAddressEnvironment:      ":9191",
		controllerMaximumActiveRunsEnvironment:   "7",
		controllerEnrollmentTokenEnvironment:     "enrollment-token",
		controllerTokenEnvironment:               "controller-token",
		controllerCheckoutImageEnvironment:       "registry.example.com/slipramp-checkout:1",
		controllerHarnessImageEnvironment:        "registry.example.com/slipramp-harness:1",
		controllerGitCredentialsEnvironment:      `{"github-readonly":"github-readonly"}`,
		controllerExecutionNamespacesEnvironment: `["runs-a","runs-b"]`,
		controllerKargoIntegrationsEnvironment:   `[{"name":"prod","stageNamespaces":["kargo-core"]}]`,
	}

	cfg, err := config.LoadController(mapEnvironment(environment))
	if err != nil {
		t.Fatalf("LoadController() error = %v", err)
	}

	if cfg.ClusterName != "colo" {
		t.Errorf("ClusterName = %q, want %q", cfg.ClusterName, "colo")
	}

	if cfg.ServerURL != "https://slipramp.example.com" {
		t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, "https://slipramp.example.com")
	}
	if cfg.MetricsAddress != ":9191" {
		t.Errorf("MetricsAddress = %q, want %q", cfg.MetricsAddress, ":9191")
	}
	if cfg.MaximumActiveRuns != 7 {
		t.Errorf("MaximumActiveRuns = %d, want 7", cfg.MaximumActiveRuns)
	}

	if cfg.BearerToken != "controller-token" {
		t.Error("BearerToken does not match environment")
	}

	if cfg.EnrollmentToken != "enrollment-token" {
		t.Error("EnrollmentToken does not match environment")
	}

	if cfg.CheckoutImage != "registry.example.com/slipramp-checkout:1" {
		t.Errorf("CheckoutImage = %q", cfg.CheckoutImage)
	}

	if cfg.HarnessImage != "registry.example.com/slipramp-harness:1" {
		t.Errorf("HarnessImage = %q", cfg.HarnessImage)
	}

	if cfg.GitCredentials["github-readonly"] != "github-readonly" {
		t.Error("GitCredentials does not contain github-readonly")
	}

	if len(cfg.ExecutionNamespaces) != 2 || cfg.ExecutionNamespaces[1] != "runs-b" {
		t.Errorf("ExecutionNamespaces = %#v", cfg.ExecutionNamespaces)
	}
	if len(cfg.KargoIntegrations) != 1 || cfg.KargoIntegrations[0].Name != "prod" ||
		cfg.KargoIntegrations[0].StageNamespaces[0] != "kargo-core" {
		t.Errorf("KargoIntegrations = %#v", cfg.KargoIntegrations)
	}
}

func TestLoadControllerDefaultsMetricsAddress(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadController(mapEnvironment(validControllerEnvironment()))
	if err != nil {
		t.Fatalf("LoadController() error = %v", err)
	}
	if cfg.MetricsAddress != ":9091" {
		t.Errorf("MetricsAddress = %q, want %q", cfg.MetricsAddress, ":9091")
	}
}

func TestLoadControllerRejectsInvalidEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(map[string]string)
		wantError string
	}{
		{
			name: "missing cluster",
			configure: func(environment map[string]string) {
				delete(environment, controllerClusterEnvironment)
			},
			wantError: "cluster name",
		},
		{
			name: "invalid Git credentials",
			configure: func(environment map[string]string) {
				environment[controllerGitCredentialsEnvironment] = "not-json"
			},
			wantError: controllerGitCredentialsEnvironment,
		},
		{
			name: "invalid metrics address",
			configure: func(environment map[string]string) {
				environment[controllerMetricsAddressEnvironment] = "localhost"
			},
			wantError: "metrics address",
		},
		{
			name: "invalid maximum active Runs",
			configure: func(environment map[string]string) {
				environment[controllerMaximumActiveRunsEnvironment] = "many"
			},
			wantError: controllerMaximumActiveRunsEnvironment,
		},
		{
			name: "invalid execution namespaces",
			configure: func(environment map[string]string) {
				environment[controllerExecutionNamespacesEnvironment] = "not-json"
			},
			wantError: controllerExecutionNamespacesEnvironment,
		},
		{
			name: "invalid Kargo integrations",
			configure: func(environment map[string]string) {
				environment[controllerKargoIntegrationsEnvironment] = "not-json"
			},
			wantError: controllerKargoIntegrationsEnvironment,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			environment := validControllerEnvironment()
			test.configure(environment)

			_, err := config.LoadController(mapEnvironment(environment))
			testcheck.ErrorContains(t, err, test.wantError)
		})
	}
}

func validControllerEnvironment() map[string]string {
	return map[string]string{
		controllerClusterEnvironment:             "colo",
		controllerServerEnvironment:              "https://slipramp.example.com",
		controllerEnrollmentTokenEnvironment:     "enrollment-token",
		controllerTokenEnvironment:               "controller-token",
		controllerCheckoutImageEnvironment:       "checkout:1",
		controllerHarnessImageEnvironment:        "harness:1",
		controllerExecutionNamespacesEnvironment: `["slipramp-runs"]`,
	}
}

func TestLoadControllerRequiresEnvironmentLookup(t *testing.T) {
	t.Parallel()

	_, err := config.LoadController(nil)
	testcheck.ErrorContains(t, err, "environment lookup")
}
