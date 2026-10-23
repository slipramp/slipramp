package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const commitHistoryFetchDepth = 500

type GitCommitHistoryFetcher struct {
	credentials RefreshCredentials
}

func NewGitCommitHistoryFetcher(credentials RefreshCredentials) *GitCommitHistoryFetcher {
	return &GitCommitHistoryFetcher{credentials: credentials}
}

func (f *GitCommitHistoryFetcher) FetchCommitHistory(
	ctx context.Context,
	repository Repository,
	desiredHeadSHA string,
	indexedHeadSHA *string,
) (FetchedCommitHistory, error) {
	if !validCommitSHA(desiredHeadSHA) {
		return FetchedCommitHistory{}, fmt.Errorf("invalid desired Git commit %q", desiredHeadSHA)
	}
	if indexedHeadSHA != nil && !validCommitSHA(*indexedHeadSHA) {
		return FetchedCommitHistory{}, fmt.Errorf("invalid indexed Git commit %q", *indexedHeadSHA)
	}

	gitConfigFile, err := f.credentials.gitConfigFile(repository.RefreshCredentialName)
	if err != nil {
		return FetchedCommitHistory{}, err
	}

	directory, err := os.MkdirTemp("", "slipramp-commit-history-*")
	if err != nil {
		return FetchedCommitHistory{}, fmt.Errorf("create commit history directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()

	environment := gitEnvironment(gitConfigFile)
	if _, err := runGit(ctx, environment, "init", "--quiet", directory); err != nil {
		return FetchedCommitHistory{}, err
	}
	if err := fetchCommitHistoryDepth(
		ctx,
		environment,
		directory,
		repository.CloneURL,
		desiredHeadSHA,
		false,
	); err != nil {
		return FetchedCommitHistory{}, err
	}

	output, err := runGit(ctx, environment, "-C", directory, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return FetchedCommitHistory{}, fmt.Errorf("resolve commit history head: %w", err)
	}
	headSHA := strings.TrimSpace(output)
	if headSHA != desiredHeadSHA {
		return FetchedCommitHistory{}, fmt.Errorf(
			"fetched commit history head %q does not match desired commit %q",
			headSHA,
			desiredHeadSHA,
		)
	}

	for {
		if indexedHeadSHA != nil {
			available, availableErr := gitCommitAvailable(ctx, environment, directory, *indexedHeadSHA)
			if availableErr != nil {
				return FetchedCommitHistory{}, availableErr
			}
			if available {
				ancestor, ancestorErr := gitCommitIsAncestor(
					ctx,
					environment,
					directory,
					*indexedHeadSHA,
					headSHA,
				)
				if ancestorErr != nil {
					return FetchedCommitHistory{}, ancestorErr
				}
				if ancestor {
					commits, logErr := readCommitHistory(
						ctx,
						environment,
						directory,
						repository.ID,
						*indexedHeadSHA+".."+headSHA,
					)
					if logErr != nil {
						return FetchedCommitHistory{}, logErr
					}

					return FetchedCommitHistory{HeadSHA: headSHA, Commits: commits}, nil
				}
			}
		}

		shallow, shallowErr := isShallowRepository(ctx, environment, directory)
		if shallowErr != nil {
			return FetchedCommitHistory{}, shallowErr
		}
		if !shallow {
			commits, logErr := readCommitHistory(
				ctx,
				environment,
				directory,
				repository.ID,
				headSHA,
			)
			if logErr != nil {
				return FetchedCommitHistory{}, logErr
			}

			return FetchedCommitHistory{HeadSHA: headSHA, Commits: commits, Replace: true}, nil
		}

		if err := fetchCommitHistoryDepth(
			ctx,
			environment,
			directory,
			repository.CloneURL,
			desiredHeadSHA,
			true,
		); err != nil {
			return FetchedCommitHistory{}, err
		}
	}
}

func fetchCommitHistoryDepth(
	ctx context.Context,
	environment []string,
	directory string,
	cloneURL string,
	headSHA string,
	deepen bool,
) error {
	depthArgument := fmt.Sprintf("--depth=%d", commitHistoryFetchDepth)
	if deepen {
		depthArgument = fmt.Sprintf("--deepen=%d", commitHistoryFetchDepth)
	}
	_, err := runGit(
		ctx,
		environment,
		"-C",
		directory,
		"fetch",
		"--quiet",
		"--no-tags",
		"--filter=tree:0",
		depthArgument,
		"--",
		cloneURL,
		headSHA,
	)
	if err != nil {
		return fmt.Errorf("fetch commit history: %w", err)
	}

	return nil
}

func gitCommitAvailable(
	ctx context.Context,
	environment []string,
	directory string,
	commitSHA string,
) (bool, error) {
	command := exec.CommandContext(ctx, "git", "-C", directory, "cat-file", "-e", commitSHA+"^{commit}")
	command.Env = environment
	err := command.Run()
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	if _, ok := errors.AsType[*exec.ExitError](err); ok {
		return false, nil
	}

	return false, fmt.Errorf("inspect indexed Git commit: %w", err)
}

func gitCommitIsAncestor(
	ctx context.Context,
	environment []string,
	directory string,
	ancestorSHA string,
	descendantSHA string,
) (bool, error) {
	command := exec.CommandContext(
		ctx,
		"git",
		"-C",
		directory,
		"merge-base",
		"--is-ancestor",
		ancestorSHA,
		descendantSHA,
	)
	command.Env = environment
	err := command.Run()
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	if exitError, ok := errors.AsType[*exec.ExitError](err); ok && exitError.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("compare commit history ancestry: %w", err)
}

func isShallowRepository(
	ctx context.Context,
	environment []string,
	directory string,
) (bool, error) {
	output, err := runGit(ctx, environment, "-C", directory, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, fmt.Errorf("inspect commit history depth: %w", err)
	}

	switch strings.TrimSpace(output) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("git returned invalid shallow repository state %q", strings.TrimSpace(output))
	}
}

func readCommitHistory(
	ctx context.Context,
	environment []string,
	directory string,
	repositoryID RepositoryID,
	revision string,
) ([]Commit, error) {
	output, err := runGit(
		ctx,
		environment,
		"-C",
		directory,
		"log",
		"-z",
		"--format=%H%x00%P%x00%s%x00%an%x00%aI%x00%cI",
		revision,
	)
	if err != nil {
		return nil, fmt.Errorf("read commit history metadata: %w", err)
	}
	if output == "" {
		return nil, nil
	}

	fields := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	const fieldsPerCommit = 6
	if len(fields)%fieldsPerCommit != 0 {
		return nil, errors.New("git returned malformed commit history metadata")
	}

	commits := make([]Commit, 0, len(fields)/fieldsPerCommit)
	for offset := 0; offset < len(fields); offset += fieldsPerCommit {
		sha := fields[offset]
		if !validCommitSHA(sha) {
			return nil, fmt.Errorf("git returned invalid commit history SHA %q", sha)
		}
		authoredAt, parseErr := time.Parse(time.RFC3339, fields[offset+4])
		if parseErr != nil {
			return nil, fmt.Errorf("parse commit history author time: %w", parseErr)
		}
		committedAt, parseErr := time.Parse(time.RFC3339, fields[offset+5])
		if parseErr != nil {
			return nil, fmt.Errorf("parse commit history commit time: %w", parseErr)
		}

		title := strings.ToValidUTF8(fields[offset+2], "�")
		authorName := strings.ToValidUTF8(fields[offset+3], "�")
		authoredAt = authoredAt.UTC()
		committedAt = committedAt.UTC()
		commits = append(commits, Commit{
			RepositoryID: repositoryID,
			SHA:          sha,
			Title:        &title,
			AuthorName:   &authorName,
			AuthoredAt:   &authoredAt,
			CommittedAt:  &committedAt,
		})
	}

	return commits, nil
}

var _ CommitHistoryFetcher = (*GitCommitHistoryFetcher)(nil)
