package server

import (
	"strings"
	"testing"
	"time"
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
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

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
