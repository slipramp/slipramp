package repository

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var commitSHAPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type GitFetcher struct {
	credentials RefreshCredentials
}

type FetchedCommit struct {
	Commit       Commit
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

func (f *GitFetcher) Fetch(ctx context.Context, repository Repository) (FetchedCommit, error) {
	gitConfigFile, err := f.credentials.gitConfigFile(repository.RefreshCredentialName)
	if err != nil {
		return FetchedCommit{}, err
	}

	checkoutDirectory, err := os.MkdirTemp("", "slipramp-repository-*")
	if err != nil {
		return FetchedCommit{}, fmt.Errorf("create repository refresh directory: %w", err)
	}
	keepCheckout := false
	defer func() {
		if !keepCheckout {
			_ = os.RemoveAll(checkoutDirectory)
		}
	}()

	environment := gitEnvironment(gitConfigFile)
	if _, err := runGit(ctx, environment, "init", "--quiet", checkoutDirectory); err != nil {
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
		return FetchedCommit{}, fmt.Errorf("fetch branch %q: %w", repository.Branch, err)
	}

	output, err := runGit(ctx, environment, "-C", checkoutDirectory, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return FetchedCommit{}, fmt.Errorf("resolve fetched commit: %w", err)
	}

	commitSHA := strings.TrimSpace(output)
	if !ValidCommitSHA(commitSHA) {
		return FetchedCommit{}, fmt.Errorf("git returned invalid commit %q", commitSHA)
	}
	commit := Commit{
		RepositoryID: repository.ID,
		SHA:          commitSHA,
	}
	if repository.CommitIndexing != CommitIndexingDisabled {
		if metadata, metadataErr := readCommitMetadata(ctx, environment, checkoutDirectory, commitSHA); metadataErr == nil {
			commit.Title = &metadata.title
			commit.AuthorName = &metadata.authorName
			commit.AuthoredAt = &metadata.authoredAt
			commit.CommittedAt = &metadata.committedAt
		}
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
		return FetchedCommit{}, fmt.Errorf("materialize fetched commit: %w", err)
	}

	keepCheckout = true

	return FetchedCommit{
		Commit:       commit,
		ChangedPaths: changedPaths,
		Files:        os.DirFS(checkoutDirectory),
		cleanup: func() error {
			return os.RemoveAll(checkoutDirectory)
		},
	}, nil
}

type commitMetadata struct {
	title       string
	authorName  string
	authoredAt  time.Time
	committedAt time.Time
}

func readCommitMetadata(
	ctx context.Context,
	environment []string,
	checkoutDirectory string,
	commitSHA string,
) (commitMetadata, error) {
	output, err := runGit(
		ctx,
		environment,
		"-C",
		checkoutDirectory,
		"show",
		"--quiet",
		"--format=%H%x00%s%x00%an%x00%aI%x00%cI%x00",
		commitSHA,
	)
	if err != nil {
		return commitMetadata{}, fmt.Errorf("read fetched commit metadata: %w", err)
	}

	fields := strings.Split(strings.TrimSuffix(output, "\n"), "\x00")
	if len(fields) != 6 || fields[5] != "" || fields[0] != commitSHA {
		return commitMetadata{}, errors.New("git returned malformed commit metadata")
	}
	authoredAt, err := time.Parse(time.RFC3339, fields[3])
	if err != nil {
		return commitMetadata{}, fmt.Errorf("parse fetched commit author time: %w", err)
	}
	committedAt, err := time.Parse(time.RFC3339, fields[4])
	if err != nil {
		return commitMetadata{}, fmt.Errorf("parse fetched commit time: %w", err)
	}

	return commitMetadata{
		title:       strings.ToValidUTF8(fields[1], "�"),
		authorName:  strings.ToValidUTF8(fields[2], "�"),
		authoredAt:  authoredAt.UTC(),
		committedAt: committedAt.UTC(),
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

func runGit(ctx context.Context, environment []string, arguments ...string) (string, error) {
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
	environment = append(environment, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")

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

// ValidCommitSHA reports whether value is a lowercase full Git object ID.
func ValidCommitSHA(value string) bool {
	return commitSHAPattern.MatchString(value)
}

var _ Fetcher = (*GitFetcher)(nil)
