package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type RefreshStatus string

const (
	RefreshStatusPending   RefreshStatus = "pending"
	RefreshStatusSucceeded RefreshStatus = "succeeded"
	RefreshStatusFailed    RefreshStatus = "failed"
)

type RefreshState struct {
	Status        RefreshStatus
	CommitSHA     *string
	AttemptedAt   *time.Time
	SucceededAt   *time.Time
	FailureReason *string
}

type RefreshResult struct {
	CommitSHA     *string
	AttemptedAt   time.Time
	FailureReason *string
}

type RefreshStore interface {
	ListRepositories(ctx context.Context) ([]Repository, error)
	RecordRepositoryRefresh(
		ctx context.Context,
		repositoryID RepositoryID,
		result RefreshResult,
	) error
}

type Fetcher interface {
	Fetch(ctx context.Context, repository Repository) (string, error)
}

type Refresher struct {
	repositories RefreshStore
	fetcher      Fetcher
	interval     time.Duration
	fetchTimeout time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

func NewRefresher(
	repositories RefreshStore,
	fetcher Fetcher,
	interval time.Duration,
	fetchTimeout time.Duration,
	logger *slog.Logger,
) *Refresher {
	return &Refresher{
		repositories: repositories,
		fetcher:      fetcher,
		interval:     interval,
		fetchTimeout: fetchTimeout,
		logger:       logger,
		now:          time.Now,
	}
}

func (r *Refresher) Run(ctx context.Context) {
	r.refreshAll(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshAll(ctx)
		}
	}
}

func (r *Refresher) refreshAll(ctx context.Context) {
	repositories, err := r.repositories.ListRepositories(ctx)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("list repositories for refresh", "error", err)
		}

		return
	}

	for _, item := range repositories {
		if ctx.Err() != nil {
			return
		}

		r.refresh(ctx, item)
	}
}

func (r *Refresher) refresh(ctx context.Context, item Repository) {
	attemptedAt := r.now().UTC()
	fetchContext, cancel := context.WithTimeout(ctx, r.fetchTimeout)
	commitSHA, fetchErr := r.fetcher.Fetch(fetchContext, item)
	cancel()

	if ctx.Err() != nil {
		return
	}

	result := RefreshResult{AttemptedAt: attemptedAt}
	if fetchErr == nil {
		result.CommitSHA = &commitSHA
	} else {
		failureReason := fetchErr.Error()
		result.FailureReason = &failureReason
	}

	err := r.repositories.RecordRepositoryRefresh(ctx, item.ID, result)
	if errors.Is(err, ErrNotFound) {
		return
	}

	if err != nil {
		r.logger.Error(
			"record repository refresh",
			"repository",
			item.Name,
			"error",
			err,
		)

		return
	}

	if fetchErr != nil {
		r.logger.Warn(
			"repository refresh failed",
			"repository",
			item.Name,
			"error",
			fetchErr,
		)

		return
	}

	r.logger.Info(
		"repository refreshed",
		"repository",
		item.Name,
		"commit",
		commitSHA,
	)
}

func ValidateRefreshResult(result RefreshResult) error {
	if result.AttemptedAt.IsZero() {
		return errors.New("refresh attempted time is required")
	}

	if (result.CommitSHA == nil) == (result.FailureReason == nil) {
		return errors.New("refresh result must contain either a commit or a failure")
	}

	if result.CommitSHA != nil && !validCommitSHA(*result.CommitSHA) {
		return fmt.Errorf("invalid fetched Git commit %q", *result.CommitSHA)
	}

	if result.FailureReason != nil && *result.FailureReason == "" {
		return errors.New("refresh failure reason must not be empty")
	}

	return nil
}
