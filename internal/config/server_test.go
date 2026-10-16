package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/config"
)

const (
	serverAddressEnvironment         = "SLIPRAMP_SERVER_ADDRESS"
	serverShutdownTimeoutEnvironment = "SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT"
)

func TestLoadServerUsesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadServer(mapEnvironment(nil))
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}

	if cfg.Address != ":8080" {
		t.Errorf("Address = %q, want %q", cfg.Address, ":8080")
	}

	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf(
			"ShutdownTimeout = %s, want %s",
			cfg.ShutdownTimeout,
			10*time.Second,
		)
	}
}

func TestLoadServerFromEnvironment(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		serverAddressEnvironment:         "127.0.0.1:9000",
		serverShutdownTimeoutEnvironment: "25s",
	}

	cfg, err := config.LoadServer(mapEnvironment(environment))
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}

	if cfg.Address != "127.0.0.1:9000" {
		t.Errorf(
			"Address = %q, want %q",
			cfg.Address,
			"127.0.0.1:9000",
		)
	}

	if cfg.ShutdownTimeout != 25*time.Second {
		t.Errorf(
			"ShutdownTimeout = %s, want %s",
			cfg.ShutdownTimeout,
			25*time.Second,
		)
	}
}

func TestLoadServerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
		wantError   string
	}{
		{
			name:        "empty address",
			environment: map[string]string{serverAddressEnvironment: ""},
			wantError:   "validate server configuration",
		},
		{
			name:        "missing port",
			environment: map[string]string{serverAddressEnvironment: "localhost"},
			wantError:   "validate server configuration",
		},
		{
			name:        "zero port",
			environment: map[string]string{serverAddressEnvironment: ":0"},
			wantError:   "validate server configuration",
		},
		{
			name: "malformed shutdown timeout",
			environment: map[string]string{
				serverShutdownTimeoutEnvironment: "soon",
			},
			wantError: serverShutdownTimeoutEnvironment,
		},
		{
			name: "negative shutdown timeout",
			environment: map[string]string{
				serverShutdownTimeoutEnvironment: "-1s",
			},
			wantError: "validate server configuration",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.LoadServer(mapEnvironment(test.environment))
			if err == nil {
				t.Fatal("LoadServer() error = nil, want an error")
			}

			if !strings.Contains(err.Error(), test.wantError) {
				t.Errorf(
					"LoadServer() error = %q, want %q",
					err,
					test.wantError,
				)
			}
		})
	}
}

func TestLoadServerRequiresEnvironmentLookup(t *testing.T) {
	t.Parallel()

	_, err := config.LoadServer(nil)
	if err == nil {
		t.Fatal("LoadServer() error = nil, want an error")
	}

	if !strings.Contains(err.Error(), "environment lookup") {
		t.Errorf("LoadServer() error = %q, want environment lookup", err)
	}
}

func mapEnvironment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
