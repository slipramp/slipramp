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
			name: "valid plaintext HTTP",
			configure: func(settings *Config) {
				settings.ServerURL = "http://100.64.0.10:8080"
			},
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
			wantError: "HTTP or HTTPS URL",
		},
		{
			name: "unsupported server URL scheme",
			configure: func(settings *Config) {
				settings.ServerURL = "ftp://slipramp.example.com"
			},
			wantError: "HTTP or HTTPS URL",
		},
		{
			name: "server URL without host",
			configure: func(settings *Config) {
				settings.ServerURL = "https:///api"
			},
			wantError: "HTTP or HTTPS URL",
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
		{
			name: "empty enrollment token",
			configure: func(settings *Config) {
				settings.EnrollmentToken = ""
			},
			wantError: "enrollment credential",
		},
		{
			name: "invalid enrollment token",
			configure: func(settings *Config) {
				settings.EnrollmentToken = "enrollment token"
			},
			wantError: "enrollment credential",
		},
		{
			name: "empty checkout image",
			configure: func(settings *Config) {
				settings.CheckoutImage = ""
			},
			wantError: "checkout image",
		},
		{
			name: "empty harness image",
			configure: func(settings *Config) {
				settings.HarnessImage = ""
			},
			wantError: "harness image",
		},
		{
			name: "invalid Git credential name",
			configure: func(settings *Config) {
				settings.GitCredentials = map[string]string{
					"GitHub": "github-readonly",
				}
			},
			wantError: "Git credential name",
		},
		{
			name: "invalid Git credential secret name",
			configure: func(settings *Config) {
				settings.GitCredentials = map[string]string{
					"github": "GitHub Secret",
				}
			},
			wantError: "secret name",
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
		ClusterName:     "colo",
		ServerURL:       "https://slipramp.example.com",
		EnrollmentToken: "enrollment-token",
		BearerToken:     "controller-token",
		CheckoutImage:   "registry.example.com/slipramp-checkout:1",
		HarnessImage:    "registry.example.com/slipramp-harness:1",
		GitCredentials: map[string]string{
			"github-readonly": "github-readonly",
		},
	}
}
