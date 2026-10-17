package repository

import (
	"strings"
	"testing"
)

func TestRefreshCredentialsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		credentials RefreshCredentials
		wantError   string
	}{
		{
			name: "valid",
			credentials: RefreshCredentials{
				"github-server": "/var/run/secrets/slipramp/github.gitconfig",
			},
		},
		{
			name: "invalid name",
			credentials: RefreshCredentials{
				"GitHub": "/var/run/secrets/slipramp/github.gitconfig",
			},
			wantError: "refresh credential name",
		},
		{
			name: "relative path",
			credentials: RefreshCredentials{
				"github-server": "github.gitconfig",
			},
			wantError: "must be absolute",
		},
		{
			name: "empty path",
			credentials: RefreshCredentials{
				"github-server": "",
			},
			wantError: "must not be empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.credentials.Validate()
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
