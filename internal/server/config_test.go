package server

import (
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/placement"
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

			err := settings.Validate()
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("Validate() error = nil, want an error")
			}

			if !strings.Contains(err.Error(), test.wantError) {
				t.Errorf("Validate() error = %q, want %q", err, test.wantError)
			}
		})
	}
}
