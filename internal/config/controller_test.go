package config_test

import (
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/config"
)

const (
	controllerClusterEnvironment = "SLIPRAMP_CONTROLLER_CLUSTER"
	controllerServerEnvironment  = "SLIPRAMP_CONTROLLER_SERVER_URL"
	controllerTokenEnvironment   = "SLIPRAMP_CONTROLLER_TOKEN"
)

func TestLoadControllerFromEnvironment(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		controllerClusterEnvironment: "colo",
		controllerServerEnvironment:  "https://slipramp.example.com",
		controllerTokenEnvironment:   "controller-token",
	}

	cfg, err := config.LoadController(mapEnvironment(environment))
	if err != nil {
		t.Fatalf("LoadController() error = %v", err)
	}

	if cfg.ClusterName != "colo" {
		t.Errorf("ClusterName = %q, want %q", cfg.ClusterName, "colo")
	}

	if cfg.ServerURL != "https://slipramp.example.com" {
		t.Errorf(
			"ServerURL = %q, want %q",
			cfg.ServerURL,
			"https://slipramp.example.com",
		)
	}

	if cfg.BearerToken != "controller-token" {
		t.Error("BearerToken does not match environment")
	}
}

func TestLoadControllerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
		wantError   string
	}{
		{
			name: "missing cluster",
			environment: map[string]string{
				controllerServerEnvironment: "https://slipramp.example.com",
				controllerTokenEnvironment:  "controller-token",
			},
			wantError: "cluster name",
		},
		{
			name: "missing server URL",
			environment: map[string]string{
				controllerClusterEnvironment: "colo",
				controllerTokenEnvironment:   "controller-token",
			},
			wantError: "server URL",
		},
		{
			name: "missing token",
			environment: map[string]string{
				controllerClusterEnvironment: "colo",
				controllerServerEnvironment:  "https://slipramp.example.com",
			},
			wantError: "bearer token",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.LoadController(
				mapEnvironment(test.environment),
			)
			if err == nil {
				t.Fatal("LoadController() error = nil, want an error")
			}

			if !strings.Contains(err.Error(), test.wantError) {
				t.Errorf(
					"LoadController() error = %q, want %q",
					err,
					test.wantError,
				)
			}
		})
	}
}

func TestLoadControllerRequiresEnvironmentLookup(t *testing.T) {
	t.Parallel()

	_, err := config.LoadController(nil)
	if err == nil {
		t.Fatal("LoadController() error = nil, want an error")
	}

	if !strings.Contains(err.Error(), "environment lookup") {
		t.Errorf("LoadController() error = %q, want environment lookup", err)
	}
}
