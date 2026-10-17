package server

import (
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	settings := DefaultConfig()

	if settings.Address != ":8080" {
		t.Errorf("Address = %q, want %q", settings.Address, ":8080")
	}

	if settings.ShutdownTimeout != 10*time.Second {
		t.Errorf(
			"ShutdownTimeout = %s, want %s",
			settings.ShutdownTimeout,
			10*time.Second,
		)
	}

	if settings.RepositoryRefreshInterval != time.Minute {
		t.Errorf(
			"RepositoryRefreshInterval = %s, want %s",
			settings.RepositoryRefreshInterval,
			time.Minute,
		)
	}

	if settings.RepositoryFetchTimeout != 30*time.Second {
		t.Errorf(
			"RepositoryFetchTimeout = %s, want %s",
			settings.RepositoryFetchTimeout,
			30*time.Second,
		)
	}

	if err := settings.Database.Validate(); err != nil {
		t.Errorf("Database.Validate() error = %v", err)
	}

	if err := settings.ObjectStore.Validate(); err != nil {
		t.Errorf("ObjectStore.Validate() error = %v", err)
	}

	if settings.Placements.Len() != 0 {
		t.Errorf("Placements.Len() = %d, want 0", settings.Placements.Len())
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	configuredPlacements, err := placement.NewSet(map[string]placement.Config{
		"infrastructure": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-runs",
		},
	})
	if err != nil {
		t.Fatalf("placement.NewSet() error = %v", err)
	}

	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{
			name: "valid",
		},
		{
			name: "empty address",
			configure: func(settings *Config) {
				settings.Address = ""
			},
			wantError: "address",
		},
		{
			name: "missing port",
			configure: func(settings *Config) {
				settings.Address = "localhost"
			},
			wantError: "address",
		},
		{
			name: "zero port",
			configure: func(settings *Config) {
				settings.Address = ":0"
			},
			wantError: "address",
		},
		{
			name: "negative shutdown timeout",
			configure: func(settings *Config) {
				settings.ShutdownTimeout = -1 * time.Second
			},
			wantError: "shutdown timeout",
		},
		{
			name: "invalid repository credential",
			configure: func(settings *Config) {
				settings.RepositoryRefreshCredentials["github"] = "relative"
			},
			wantError: "repository refresh credentials",
		},
		{
			name: "invalid repository refresh interval",
			configure: func(settings *Config) {
				settings.RepositoryRefreshInterval = 0
			},
			wantError: "repository refresh interval",
		},
		{
			name: "invalid repository fetch timeout",
			configure: func(settings *Config) {
				settings.RepositoryFetchTimeout = 0
			},
			wantError: "repository fetch timeout",
		},
		{
			name: "invalid controller cluster name",
			configure: func(settings *Config) {
				settings.ControllerTokens = map[string]string{
					"Invalid Cluster": "controller-token",
				}
			},
			wantError: "cluster name",
		},
		{
			name: "empty controller token",
			configure: func(settings *Config) {
				settings.ControllerTokens = map[string]string{"colo": ""}
			},
			wantError: "controller token",
		},
		{
			name: "invalid controller token",
			configure: func(settings *Config) {
				settings.ControllerTokens = map[string]string{
					"colo": "controller token",
				}
			},
			wantError: "controller token",
		},
		{
			name: "valid placement",
			configure: func(settings *Config) {
				settings.ControllerTokens["colo"] = "controller-token"
				settings.Placements = configuredPlacements
			},
		},
		{
			name: "unregistered placement cluster",
			configure: func(settings *Config) {
				settings.Placements = configuredPlacements
			},
			wantError: "unregistered cluster",
		},
		{
			name: "invalid database",
			configure: func(settings *Config) {
				settings.Database.URL = ""
			},
			wantError: "database",
		},
		{
			name: "invalid object store",
			configure: func(settings *Config) {
				settings.ObjectStore.Endpoint = ""
			},
			wantError: "object store",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := DefaultConfig()
			if test.configure != nil {
				test.configure(&settings)
			}

			testcheck.ErrorContains(t, settings.Validate(), test.wantError)
		})
	}
}
