package repository

import (
	"strings"
	"testing"
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

			refreshCredentialName := "github-server"
			checkoutCredentialName := "github-readonly"
			cfg := Config{
				Name:                   "infrastructure",
				CloneURL:               repositoryURL,
				Branch:                 "release/production",
				RefreshCredentialName:  &refreshCredentialName,
				CheckoutCredentialName: &checkoutCredentialName,
			}

			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() error = %v", err)
			}
		})
	}
}

func TestConfigValidationRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	valid := Config{
		Name:     "infrastructure",
		CloneURL: "https://github.com/slipramp/slipramp.git",
		Branch:   "main",
	}
	invalidCredential := "Embedded Secret"

	tests := []struct {
		name          string
		cfg           Config
		errorContains string
	}{
		{
			name: "repository name",
			cfg: Config{
				Name:     "Infrastructure",
				CloneURL: valid.CloneURL,
				Branch:   valid.Branch,
			},
			errorContains: "repository name",
		},
		{
			name: "unsupported URL scheme",
			cfg: Config{
				Name:     valid.Name,
				CloneURL: "http://example.com/repository.git",
				Branch:   valid.Branch,
			},
			errorContains: "scheme must be https or ssh",
		},
		{
			name: "embedded HTTPS credential",
			cfg: Config{
				Name:     valid.Name,
				CloneURL: "https://token@example.com/repository.git",
				Branch:   valid.Branch,
			},
			errorContains: "embedded HTTPS credentials",
		},
		{
			name: "invalid branch",
			cfg: Config{
				Name:     valid.Name,
				CloneURL: valid.CloneURL,
				Branch:   "feature..broken",
			},
			errorContains: "Git branch name",
		},
		{
			name: "branch beginning with dash",
			cfg: Config{
				Name:     valid.Name,
				CloneURL: valid.CloneURL,
				Branch:   "-main",
			},
			errorContains: "Git branch name",
		},
		{
			name: "invalid credential reference",
			cfg: Config{
				Name:                  valid.Name,
				CloneURL:              valid.CloneURL,
				Branch:                valid.Branch,
				RefreshCredentialName: &invalidCredential,
			},
			errorContains: "refresh credential",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.errorContains) {
				t.Errorf(
					"Validate() error = %v, want error containing %q",
					err,
					test.errorContains,
				)
			}
		})
	}
}
