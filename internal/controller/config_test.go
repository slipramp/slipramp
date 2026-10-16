package controller

import (
	"strings"
	"testing"
)

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
			name: "empty cluster name",
			configure: func(settings *Config) {
				settings.ClusterName = ""
			},
			wantError: "cluster name",
		},
		{
			name: "relative server URL",
			configure: func(settings *Config) {
				settings.ServerURL = "/api"
			},
			wantError: "HTTPS URL",
		},
		{
			name: "insecure server URL",
			configure: func(settings *Config) {
				settings.ServerURL = "http://slipramp.example.com"
			},
			wantError: "HTTPS URL",
		},
		{
			name: "server URL without host",
			configure: func(settings *Config) {
				settings.ServerURL = "https:///api"
			},
			wantError: "HTTPS URL",
		},
		{
			name: "server URL with user information",
			configure: func(settings *Config) {
				settings.ServerURL = "https://user@slipramp.example.com"
			},
			wantError: "user information",
		},
		{
			name: "empty bearer token",
			configure: func(settings *Config) {
				settings.BearerToken = ""
			},
			wantError: "bearer token",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := validConfig()
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

func validConfig() Config {
	return Config{
		ClusterName: "colo",
		ServerURL:   "https://slipramp.example.com",
		BearerToken: "controller-token",
	}
}
