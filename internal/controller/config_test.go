package controller

import (
	"testing"

	"github.com/slipramp/slipramp/internal/testcheck"
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
			name: "invalid cluster name",
			configure: func(settings *Config) {
				settings.ClusterName = "Invalid Cluster"
			},
			wantError: "cluster name",
		},
		{
			name: "cluster name ending in hyphen",
			configure: func(settings *Config) {
				settings.ClusterName = "colo-"
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
			name: "server URL with query",
			configure: func(settings *Config) {
				settings.ServerURL = "https://slipramp.example.com?debug=true"
			},
			wantError: "query or fragment",
		},
		{
			name: "empty bearer token",
			configure: func(settings *Config) {
				settings.BearerToken = ""
			},
			wantError: "bearer token",
		},
		{
			name: "invalid bearer token",
			configure: func(settings *Config) {
				settings.BearerToken = "controller token"
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

			testcheck.ErrorContains(t, settings.Validate(), test.wantError)
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
