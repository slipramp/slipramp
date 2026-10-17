package repository

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var commitSHAPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type GitFetcher struct {
	credentials RefreshCredentials
}

type FetchedCommit struct {
	CommitSHA    string
	ChangedPaths []string
	Files        fs.FS
	cleanup      func() error
}

func (c FetchedCommit) Close() error {
	if c.cleanup == nil {
		return nil
	}

	return c.cleanup()
}

func NewGitFetcher(credentials RefreshCredentials) *GitFetcher {
	return &GitFetcher{credentials: credentials}
}

func (f *GitFetcher) Fetch(
	ctx context.Context,
	repository Repository,
) (FetchedCommit, error) {
	gitConfigFile, err := f.credentials.gitConfigFile(
		repository.RefreshCredentialName,
	)
	if err != nil {
		return FetchedCommit{}, err
	}

	checkoutDirectory, err := os.MkdirTemp("", "slipramp-repository-*")
	if err != nil {
		return FetchedCommit{}, fmt.Errorf(
			"create repository refresh directory: %w",
			err,
		)
	}
	keepCheckout := false
	defer func() {
		if !keepCheckout {
			_ = os.RemoveAll(checkoutDirectory)
		}
	}()

	environment := gitEnvironment(gitConfigFile)
	if _, err := runGit(
		ctx,
		environment,
		"init",
		"--quiet",
		checkoutDirectory,
	); err != nil {
		return FetchedCommit{}, err
	}

	branchReference := "refs/heads/" + repository.Branch
	if _, err := runGit(
		ctx,
		environment,
		"-C",
		checkoutDirectory,
		"fetch",
		"--quiet",
		"--no-tags",
		"--depth=1",
		"--",
		repository.CloneURL,
		branchReference,
	); err != nil {
		return FetchedCommit{}, fmt.Errorf(
			"fetch branch %q: %w",
			repository.Branch,
			err,
		)
	}

	output, err := runGit(
		ctx,
		environment,
		"-C",
		checkoutDirectory,
		"rev-parse",
		"--verify",
		"FETCH_HEAD^{commit}",
	)
	if err != nil {
		return FetchedCommit{}, fmt.Errorf("resolve fetched commit: %w", err)
	}

	commitSHA := strings.TrimSpace(output)
	if !validCommitSHA(commitSHA) {
		return FetchedCommit{}, fmt.Errorf(
			"git returned invalid commit %q",
			commitSHA,
		)
	}

	changedPaths, err := changedRepositoryPaths(
		ctx,
		environment,
		checkoutDirectory,
		repository.CloneURL,
		repository.ActiveCatalogCommitSHA,
		commitSHA,
	)
	if err != nil {
		return FetchedCommit{}, err
	}

	if _, err := runGit(
		ctx,
		environment,
		"-C",
		checkoutDirectory,
		"checkout",
		"--quiet",
		"--detach",
		commitSHA,
	); err != nil {
		return FetchedCommit{}, fmt.Errorf(
			"materialize fetched commit: %w",
			err,
		)
	}

	keepCheckout = true

	return FetchedCommit{
		CommitSHA:    commitSHA,
		ChangedPaths: changedPaths,
		Files:        os.DirFS(checkoutDirectory),
		cleanup: func() error {
			return os.RemoveAll(checkoutDirectory)
		},
	}, nil
}

func changedRepositoryPaths(
	ctx context.Context,
	environment []string,
	checkoutDirectory string,
	cloneURL string,
	previousCommitSHA *string,
	commitSHA string,
) ([]string, error) {
	if previousCommitSHA == nil || *previousCommitSHA == commitSHA {
		return nil, nil
	}

	if _, err := runGit(
		ctx,
		environment,
		"-C",
		checkoutDirectory,
		"fetch",
		"--quiet",
		"--no-tags",
		"--depth=1",
		"--",
		cloneURL,
		*previousCommitSHA,
	); err != nil {
		return nil, fmt.Errorf("fetch previous accepted commit: %w", err)
	}

	output, err := runGit(
		ctx,
		environment,
		"-C",
		checkoutDirectory,
		"diff",
		"--name-only",
		"--no-renames",
		"-z",
		*previousCommitSHA,
		commitSHA,
		"--",
	)
	if err != nil {
		return nil, fmt.Errorf("compare indexed commits: %w", err)
	}

	return splitNullTerminatedPaths(output), nil
}

func splitNullTerminatedPaths(output string) []string {
	if output == "" {
		return nil
	}

	paths := strings.Split(output, "\x00")
	if paths[len(paths)-1] == "" {
		paths = paths[:len(paths)-1]
	}

	return paths
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
