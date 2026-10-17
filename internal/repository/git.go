package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var commitSHAPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type GitFetcher struct {
	credentials RefreshCredentials
}

func NewGitFetcher(credentials RefreshCredentials) *GitFetcher {
	return &GitFetcher{credentials: credentials}
}

func (f *GitFetcher) Fetch(
	ctx context.Context,
	repository Repository,
) (string, error) {
	gitConfigFile, err := f.credentials.gitConfigFile(
		repository.RefreshCredentialName,
	)
	if err != nil {
		return "", err
	}

	checkoutDirectory, err := os.MkdirTemp("", "slipramp-repository-*")
	if err != nil {
		return "", fmt.Errorf("create repository refresh directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(checkoutDirectory)
	}()

	environment := gitEnvironment(gitConfigFile)
	if _, err := runGit(
		ctx,
		environment,
		"init",
		"--bare",
		"--quiet",
		checkoutDirectory,
	); err != nil {
		return "", err
	}

	branchReference := "refs/heads/" + repository.Branch
	if _, err := runGit(
		ctx,
		environment,
		"--git-dir",
		checkoutDirectory,
		"fetch",
		"--quiet",
		"--no-tags",
		"--depth=1",
		"--",
		repository.CloneURL,
		branchReference,
	); err != nil {
		return "", fmt.Errorf(
			"fetch branch %q: %w",
			repository.Branch,
			err,
		)
	}

	output, err := runGit(
		ctx,
		environment,
		"--git-dir",
		checkoutDirectory,
		"rev-parse",
		"--verify",
		"FETCH_HEAD^{commit}",
	)
	if err != nil {
		return "", fmt.Errorf("resolve fetched commit: %w", err)
	}

	commitSHA := strings.TrimSpace(output)
	if !validCommitSHA(commitSHA) {
		return "", fmt.Errorf("git returned invalid commit %q", commitSHA)
	}

	return commitSHA, nil
}

func runGit(
	ctx context.Context,
	environment []string,
	arguments ...string,
) (string, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Env = environment

	output, err := command.Output()
	if err == nil {
		return string(output), nil
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	return "", fmt.Errorf("run Git: %w", err)
}

func gitEnvironment(gitConfigFile string) []string {
	environment := removeGitOverrides(os.Environ())
	environment = append(
		environment,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)

	if gitConfigFile == "" {
		gitConfigFile = os.DevNull
	}

	return append(environment, "GIT_CONFIG_GLOBAL="+gitConfigFile)
}

func removeGitOverrides(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, variable := range environment {
		if strings.HasPrefix(variable, "GIT_") {
			continue
		}

		filtered = append(filtered, variable)
	}

	return filtered
}

func validCommitSHA(value string) bool {
	return commitSHAPattern.MatchString(value)
}

var _ Fetcher = (*GitFetcher)(nil)
