package store

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigIsValid(t *testing.T) {
	t.Parallel()

	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() error = %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{name: "valid"},
		{
			name: "empty URL",
			configure: func(cfg *Config) {
				cfg.URL = ""
			},
			wantError: "database URL",
		},
		{
			name: "missing database",
			configure: func(cfg *Config) {
				cfg.URL = "postgres://localhost"
			},
			wantError: "select a database",
		},
		{
			name: "invalid timeout",
			configure: func(cfg *Config) {
				cfg.ConnectTimeout = -1 * time.Second
			},
			wantError: "connect timeout",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := DefaultConfig()
			if test.configure != nil {
				test.configure(&cfg)
			}

			err := cfg.Validate()
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %v, want %q", err, test.wantError)
			}
		})
	}
}
