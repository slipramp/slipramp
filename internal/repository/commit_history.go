package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const DefaultCommitHistoryIndexInterval = 30 * time.Second

type FetchedCommitHistory struct {
	HeadSHA string
	Commits []Commit
	Replace bool
}

type CommitHistoryResult struct {
	FetchedCommitHistory
	AttemptedAt time.Time
}

type CommitHistoryStore interface {
	ListRepositories(ctx context.Context) ([]Repository, error)
	RecordCommitHistory(ctx context.Context, repositoryID RepositoryID, result CommitHistoryResult) error
	RecordCommitHistoryFailure(
		ctx context.Context,
		repositoryID RepositoryID,
		attemptedHeadSHA string,
		attemptedAt time.Time,
		failureReason string,
	) error
}

type CommitHistoryFetcher interface {
	FetchCommitHistory(
		ctx context.Context,
		repository Repository,
		desiredHeadSHA string,
		indexedHeadSHA *string,
	) (FetchedCommitHistory, error)
}

type CommitHistoryIndexer struct {
	repositories CommitHistoryStore
	fetcher      CommitHistoryFetcher
	interval     time.Duration
	fetchTimeout time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

func NewCommitHistoryIndexer(
	repositories CommitHistoryStore,
	fetcher CommitHistoryFetcher,
	interval time.Duration,
	fetchTimeout time.Duration,
	logger *slog.Logger,
) *CommitHistoryIndexer {
	return &CommitHistoryIndexer{
		repositories: repositories,
		fetcher:      fetcher,
		interval:     interval,
		fetchTimeout: fetchTimeout,
		logger:       logger.With("component", "repository_commit_history_indexer"),
		now:          time.Now,
	}
}

func (i *CommitHistoryIndexer) Run(ctx context.Context) {
	i.indexAll(ctx)

	ticker := time.NewTicker(i.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.indexAll(ctx)
		}
	}
}

func (i *CommitHistoryIndexer) indexAll(ctx context.Context) {
	repositories, err := i.repositories.ListRepositories(ctx)
	if err != nil {
		if ctx.Err() == nil {
			i.logger.Error("list repositories for commit history indexing", "error", err)
		}

		return
	}

	for _, item := range repositories {
		if ctx.Err() != nil {
			return
		}
		if item.CommitIndexing != CommitIndexingTrackedBranchHistory ||
			item.CommitHistoryIndex == nil ||
			item.CommitHistoryIndex.DesiredHeadSHA == nil ||
			(item.CommitHistoryIndex.IndexedHeadSHA != nil &&
				*item.CommitHistoryIndex.IndexedHeadSHA == *item.CommitHistoryIndex.DesiredHeadSHA) {
			continue
		}

		i.index(ctx, item)
	}
}

func (i *CommitHistoryIndexer) index(ctx context.Context, item Repository) {
	index := item.CommitHistoryIndex
	desiredHeadSHA := *index.DesiredHeadSHA
	attemptedAt := i.now().UTC()

	fetchContext, cancel := context.WithTimeout(ctx, i.fetchTimeout)
	history, fetchErr := i.fetcher.FetchCommitHistory(
		fetchContext,
		item,
		desiredHeadSHA,
		index.IndexedHeadSHA,
	)
	cancel()
	if ctx.Err() != nil {
		return
	}

	if fetchErr != nil {
		err := i.repositories.RecordCommitHistoryFailure(
			ctx,
			item.ID,
			desiredHeadSHA,
			attemptedAt,
			fetchErr.Error(),
		)
		if err != nil && !errors.Is(err, ErrNotFound) {
			i.logger.Error(
				"record repository commit history failure",
				"repository", item.Name,
				"error", err,
			)
		}
		i.logger.Warn(
			"repository commit history indexing failed",
			"repository", item.Name,
			"error", fetchErr,
		)

		return
	}

	result := CommitHistoryResult{
		FetchedCommitHistory: history,
		AttemptedAt:          attemptedAt,
	}
	if err := i.repositories.RecordCommitHistory(ctx, item.ID, result); err != nil {
		if !errors.Is(err, ErrNotFound) {
			i.logger.Error(
				"record repository commit history",
				"repository", item.Name,
				"error", err,
			)
		}

		return
	}

	i.logger.Info(
		"repository commit history indexed",
		"repository", item.Name,
		"commit", history.HeadSHA,
		"indexed_commits", len(history.Commits),
		"replaced", history.Replace,
	)
}

func ValidateCommitHistoryResult(result CommitHistoryResult) error {
	if result.AttemptedAt.IsZero() {
		return errors.New("commit history attempted time is required")
	}
	if !ValidCommitSHA(result.HeadSHA) {
		return fmt.Errorf("invalid commit history head %q", result.HeadSHA)
	}
	for _, commit := range result.Commits {
		if !ValidCommitSHA(commit.SHA) {
			return fmt.Errorf("invalid commit history commit %q", commit.SHA)
		}
		if commit.Title == nil || commit.AuthorName == nil ||
			commit.AuthoredAt == nil || commit.CommittedAt == nil {
			return fmt.Errorf("commit history metadata for %q is incomplete", commit.SHA)
		}
	}

	return nil
}

func ValidateCommitHistoryFailure(headSHA string, attemptedAt time.Time, failureReason string) error {
	if !ValidCommitSHA(headSHA) {
		return fmt.Errorf("invalid attempted commit history head %q", headSHA)
	}
	if attemptedAt.IsZero() {
		return errors.New("commit history attempted time is required")
	}
	if failureReason == "" {
		return errors.New("commit history failure reason is required")
	}

	return nil
}
