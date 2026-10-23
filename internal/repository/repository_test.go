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
	invalidCommitURLTemplate := "https://git.example.com/team/repository/commit"

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
			name: "commit URL template without SHA placeholder",
			configure: func(cfg *Config) {
				cfg.CommitURLTemplate = &invalidCommitURLTemplate
			},
			wantError: "exactly one {sha} placeholder",
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

func TestCommitURL(t *testing.T) {
	t.Parallel()

	sha := "0123456789abcdef0123456789abcdef01234567"
	template := "https://git.internal.example/team/ops/-/commit/{sha}"
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name:     "GitHub HTTPS",
			config:   Config{CloneURL: "https://github.com/example/ops.git"},
			expected: "https://github.com/example/ops/commit/" + sha,
		},
		{
			name:     "GitLab SSH",
			config:   Config{CloneURL: "git@gitlab.com:example/ops.git"},
			expected: "https://gitlab.com/example/ops/-/commit/" + sha,
		},
		{
			name:     "configured self-hosted provider",
			config:   Config{CommitURLTemplate: &template},
			expected: "https://git.internal.example/team/ops/-/commit/" + sha,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			actual := CommitURL(test.config, sha)
			if actual == nil || *actual != test.expected {
				t.Errorf("CommitURL() = %v, want %q", actual, test.expected)
			}
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
