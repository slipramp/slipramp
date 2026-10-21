package config_test

import (
	"testing"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/testcheck"
)

const (
	controllerClusterEnvironment             = "SLIPRAMP_CONTROLLER_CLUSTER"
	controllerServerEnvironment              = "SLIPRAMP_CONTROLLER_SERVER_URL"
	controllerEnrollmentTokenEnvironment     = "SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN"
	controllerTokenEnvironment               = "SLIPRAMP_CONTROLLER_TOKEN"
	controllerCheckoutImageEnvironment       = "SLIPRAMP_CONTROLLER_CHECKOUT_IMAGE"
	controllerHarnessImageEnvironment        = "SLIPRAMP_CONTROLLER_HARNESS_IMAGE"
	controllerGitCredentialsEnvironment      = "SLIPRAMP_CONTROLLER_GIT_CREDENTIALS"
	controllerExecutionNamespacesEnvironment = "SLIPRAMP_CONTROLLER_EXECUTION_NAMESPACES"
)

func TestLoadControllerFromEnvironment(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		controllerClusterEnvironment:             "colo",
		controllerServerEnvironment:              "https://slipramp.example.com",
		controllerEnrollmentTokenEnvironment:     "enrollment-token",
		controllerTokenEnvironment:               "controller-token",
		controllerCheckoutImageEnvironment:       "registry.example.com/slipramp-checkout:1",
		controllerHarnessImageEnvironment:        "registry.example.com/slipramp-harness:1",
		controllerGitCredentialsEnvironment:      `{"github-readonly":"github-readonly"}`,
		controllerExecutionNamespacesEnvironment: `["runs-a","runs-b"]`,
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
}

func TestLoadControllerRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
		wantError   string
	}{
		{
			name: "missing cluster",
			environment: map[string]string{
				controllerServerEnvironment:              "https://slipramp.example.com",
				controllerEnrollmentTokenEnvironment:     "enrollment-token",
				controllerTokenEnvironment:               "controller-token",
				controllerCheckoutImageEnvironment:       "checkout:1",
				controllerHarnessImageEnvironment:        "harness:1",
				controllerExecutionNamespacesEnvironment: `["slipramp-runs"]`,
			},
			wantError: "cluster name",
		},
		{
			name: "missing server URL",
			environment: map[string]string{
				controllerClusterEnvironment:             "colo",
				controllerEnrollmentTokenEnvironment:     "enrollment-token",
				controllerTokenEnvironment:               "controller-token",
				controllerCheckoutImageEnvironment:       "checkout:1",
				controllerHarnessImageEnvironment:        "harness:1",
				controllerExecutionNamespacesEnvironment: `["slipramp-runs"]`,
			},
			wantError: "server URL",
		},
		{
			name: "missing token",
			environment: map[string]string{
				controllerClusterEnvironment:             "colo",
				controllerServerEnvironment:              "https://slipramp.example.com",
				controllerEnrollmentTokenEnvironment:     "enrollment-token",
				controllerCheckoutImageEnvironment:       "checkout:1",
				controllerHarnessImageEnvironment:        "harness:1",
				controllerExecutionNamespacesEnvironment: `["slipramp-runs"]`,
			},
			wantError: "bearer token",
		},
		{
			name: "missing enrollment token",
			environment: map[string]string{
				controllerClusterEnvironment:             "colo",
				controllerServerEnvironment:              "https://slipramp.example.com",
				controllerTokenEnvironment:               "controller-token",
				controllerCheckoutImageEnvironment:       "checkout:1",
				controllerHarnessImageEnvironment:        "harness:1",
				controllerExecutionNamespacesEnvironment: `["slipramp-runs"]`,
			},
			wantError: "enrollment credential",
		},
		{
			name: "missing checkout image",
			environment: map[string]string{
				controllerClusterEnvironment:             "colo",
				controllerServerEnvironment:              "https://slipramp.example.com",
				controllerEnrollmentTokenEnvironment:     "enrollment-token",
				controllerTokenEnvironment:               "controller-token",
				controllerHarnessImageEnvironment:        "harness:1",
				controllerExecutionNamespacesEnvironment: `["slipramp-runs"]`,
			},
			wantError: "checkout image",
		},
		{
			name: "missing harness image",
			environment: map[string]string{
				controllerClusterEnvironment:             "colo",
				controllerServerEnvironment:              "https://slipramp.example.com",
				controllerEnrollmentTokenEnvironment:     "enrollment-token",
				controllerTokenEnvironment:               "controller-token",
				controllerCheckoutImageEnvironment:       "checkout:1",
				controllerExecutionNamespacesEnvironment: `["slipramp-runs"]`,
			},
			wantError: "harness image",
		},
		{
			name: "invalid Git credentials",
			environment: map[string]string{
				controllerClusterEnvironment:             "colo",
				controllerServerEnvironment:              "https://slipramp.example.com",
				controllerEnrollmentTokenEnvironment:     "enrollment-token",
				controllerTokenEnvironment:               "controller-token",
				controllerCheckoutImageEnvironment:       "checkout:1",
				controllerHarnessImageEnvironment:        "harness:1",
				controllerGitCredentialsEnvironment:      "not-json",
				controllerExecutionNamespacesEnvironment: `["slipramp-runs"]`,
			},
			wantError: controllerGitCredentialsEnvironment,
		},
		{
			name: "invalid execution namespaces",
			environment: map[string]string{
				controllerClusterEnvironment:             "colo",
				controllerServerEnvironment:              "https://slipramp.example.com",
				controllerEnrollmentTokenEnvironment:     "enrollment-token",
				controllerTokenEnvironment:               "controller-token",
				controllerCheckoutImageEnvironment:       "checkout:1",
				controllerHarnessImageEnvironment:        "harness:1",
				controllerExecutionNamespacesEnvironment: "not-json",
			},
			wantError: controllerExecutionNamespacesEnvironment,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.LoadController(mapEnvironment(test.environment))
			testcheck.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestLoadControllerRequiresEnvironmentLookup(t *testing.T) {
	t.Parallel()

	_, err := config.LoadController(nil)
	testcheck.ErrorContains(t, err, "environment lookup")
}
