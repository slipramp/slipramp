package repository

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

// RefreshCredentials maps a repository's logical credential name to a mounted
// Git configuration file. The file can use Git's normal credential helpers or
// SSH command configuration without exposing secret material to Slipramp.
type RefreshCredentials map[string]string

func (c RefreshCredentials) Validate() error {
	for name, gitConfigFile := range c {
		if err := kubevalidate.DNSLabel("refresh credential name", name); err != nil {
			return err
		}

		if gitConfigFile == "" {
			return fmt.Errorf(
				"refresh credential %q Git config file must not be empty",
				name,
			)
		}

		if !filepath.IsAbs(gitConfigFile) {
			return fmt.Errorf(
				"refresh credential %q Git config file must be absolute",
				name,
			)
		}
	}

	return nil
}

func (c RefreshCredentials) gitConfigFile(name *string) (string, error) {
	if name == nil {
		return "", nil
	}

	gitConfigFile, ok := c[*name]
	if !ok {
		return "", fmt.Errorf(
			"%w: %q",
			ErrUnknownRefreshCredential,
			*name,
		)
	}

	return gitConfigFile, nil
}

func (c RefreshCredentials) validateReference(name *string) error {
	_, err := c.gitConfigFile(name)

	return err
}

var ErrUnknownRefreshCredential = errors.New("unknown refresh credential")
