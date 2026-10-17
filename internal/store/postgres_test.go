package store

import (
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/testcheck"
)

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

			testcheck.ErrorContains(t, cfg.Validate(), test.wantError)
		})
	}
}
