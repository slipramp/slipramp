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

	if err := settings.Database.Validate(); err != nil {
		t.Errorf("Database.Validate() error = %v", err)
	}

	if err := settings.ObjectStore.Validate(); err != nil {
		t.Errorf("ObjectStore.Validate() error = %v", err)
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
