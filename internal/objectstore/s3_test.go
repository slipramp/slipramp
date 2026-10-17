package objectstore

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
			name: "invalid scheme",
			configure: func(cfg *Config) {
				cfg.Endpoint = "ftp://objects.example.com"
			},
			wantError: "scheme",
		},
		{
			name: "endpoint path",
			configure: func(cfg *Config) {
				cfg.Endpoint = "https://objects.example.com/api"
			},
			wantError: "path",
		},
		{
			name: "endpoint user information",
			configure: func(cfg *Config) {
				cfg.Endpoint = "https://user:pass@objects.example.com"
			},
			wantError: "user information",
		},
		{
			name: "invalid bucket",
			configure: func(cfg *Config) {
				cfg.Bucket = "Not Valid"
			},
			wantError: "bucket",
		},
		{
			name: "empty region",
			configure: func(cfg *Config) {
				cfg.Region = ""
			},
			wantError: "region",
		},
		{
			name: "empty access key",
			configure: func(cfg *Config) {
				cfg.AccessKeyID = ""
			},
			wantError: "access key",
		},
		{
			name: "empty secret key",
			configure: func(cfg *Config) {
				cfg.SecretAccessKey = ""
			},
			wantError: "secret access key",
		},
		{
			name: "invalid timeout",
			configure: func(cfg *Config) {
				cfg.StartupTimeout = -1 * time.Second
			},
			wantError: "startup timeout",
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

func TestValidateObjectKey(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", "   ", "/runs/run-id/log"} {
		if err := validateObjectKey(key); err == nil {
			t.Errorf("validateObjectKey(%q) error = nil, want an error", key)
		}
	}

	if err := validateObjectKey("runs/run-id/log"); err != nil {
		t.Errorf("validateObjectKey() error = %v", err)
	}
}
