package repository

import (
	"testing"

	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestConfigValidationAcceptsSupportedURLs(t *testing.T) {
	t.Parallel()

	tests := []string{
		"https://github.com/slipramp/slipramp.git",
		"ssh://git@github.com/slipramp/slipramp.git",
		"git@github.com:slipramp/slipramp.git",
		"github.com:slipramp/slipramp.git",
		"git@github.com:/srv/git/slipramp.git",
	}

	for _, repositoryURL := range tests {
		t.Run(repositoryURL, func(t *testing.T) {
			t.Parallel()

			refreshCredential := "github-server"
			checkoutCredential := "github-readonly"
			cfg := validRepositoryConfig()
			cfg.CloneURL = repositoryURL
			cfg.Branch = "release/production"
			cfg.RefreshCredentialName = &refreshCredential
			cfg.CheckoutCredentialName = &checkoutCredential

			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() error = %v", err)
			}
		})
	}
}

func TestConfigValidationRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	invalidCredential := "Embedded Secret"

	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{
			name: "repository name",
			configure: func(cfg *Config) {
				cfg.Name = "Infrastructure"
			},
			wantError: "repository name",
		},
		{
			name: "unsupported URL scheme",
			configure: func(cfg *Config) {
				cfg.CloneURL = "http://example.com/repository.git"
			},
			wantError: "scheme must be https or ssh",
		},
		{
			name: "embedded HTTPS credential",
			configure: func(cfg *Config) {
				cfg.CloneURL = "https://token@example.com/repository.git"
			},
			wantError: "embedded HTTPS credentials",
		},
		{
			name: "invalid branch",
			configure: func(cfg *Config) {
				cfg.Branch = "feature..broken"
			},
			wantError: "Git branch name",
		},
		{
			name: "branch beginning with dash",
			configure: func(cfg *Config) {
				cfg.Branch = "-main"
			},
			wantError: "Git branch name",
		},
		{
			name: "invalid credential reference",
			configure: func(cfg *Config) {
				cfg.RefreshCredentialName = &invalidCredential
			},
			wantError: "refresh credential",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := validRepositoryConfig()
			test.configure(&cfg)
			testcheck.ErrorContains(t, cfg.Validate(), test.wantError)
		})
	}
}

func validRepositoryConfig() Config {
	return Config{
		Name:     "infrastructure",
		CloneURL: "https://github.com/slipramp/slipramp.git",
		Branch:   "main",
	}
}
