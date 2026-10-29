package repository

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var (
	ErrBranchNotFound     = errors.New("repository branch not found")
	ErrInvalidGitResponse = errors.New("invalid Git response")
)

type GitConnectionTester struct {
	credentials RefreshCredentials
}

func NewGitConnectionTester(credentials RefreshCredentials) *GitConnectionTester {
	return &GitConnectionTester{credentials: credentials}
}

func (t *GitConnectionTester) TestConnection(ctx context.Context, cfg ConnectionConfig) (string, error) {
	gitConfigFile, err := t.credentials.gitConfigFile(cfg.RefreshCredentialName)
	if err != nil {
		return "", err
	}

	reference := "refs/heads/" + cfg.Branch
	output, err := runGit(
		ctx,
		gitEnvironment(gitConfigFile),
		"ls-remote",
		"--exit-code",
		"--refs",
		"--",
		cfg.CloneURL,
		reference,
	)
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 2 {
			return "", ErrBranchNotFound
		}

		return "", fmt.Errorf("test Git connection: %w", err)
	}

	fields := strings.Fields(output)
	if len(fields) != 2 || fields[1] != reference || !ValidCommitSHA(fields[0]) {
		return "", ErrInvalidGitResponse
	}

	return fields[0], nil
}

var _ ConnectionTester = (*GitConnectionTester)(nil)
