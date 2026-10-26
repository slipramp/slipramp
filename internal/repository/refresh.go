package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

type RefreshStatus string

const (
	RefreshStatusPending   RefreshStatus = "pending"
	RefreshStatusSucceeded RefreshStatus = "succeeded"
	RefreshStatusFailed    RefreshStatus = "failed"
)

type RefreshState struct {
	Status           RefreshStatus
	CommitSHA        *string
	AttemptedAt      *time.Time
	SucceededAt      *time.Time
	FailureStartedAt *time.Time
	FailureReason    *string
}

type RefreshResult struct {
	Commit        *Commit
	ChangedPaths  []string
	AttemptedAt   time.Time
	FailureReason *string
	Revision      *catalog.Revision
}

type RefreshStore interface {
	ListRepositories(ctx context.Context) ([]Repository, error)
	RecordRepositoryRefresh(ctx context.Context, repositoryID RepositoryID, result RefreshResult) error
}

type Fetcher interface {
	Fetch(ctx context.Context, repository Repository) (FetchedCommit, error)
}

type RefreshAttemptResult string

const (
	RefreshAttemptSucceeded       RefreshAttemptResult = "succeeded"
	RefreshAttemptFetchFailed     RefreshAttemptResult = "fetch_failed"
	RefreshAttemptCatalogRejected RefreshAttemptResult = "catalog_rejected"
	RefreshAttemptPersistFailed   RefreshAttemptResult = "persist_failed"
)

type RefreshObserver interface {
	ObserveRepositoryRefresh(context.Context, RefreshAttemptResult, time.Duration)
}

type RefresherOption func(*Refresher)

func WithRefreshObserver(observer RefreshObserver) RefresherOption {
	return func(refresher *Refresher) {
		if observer != nil {
			refresher.observer = observer
		}
	}
}

type noopRefreshObserver struct{}

func (noopRefreshObserver) ObserveRepositoryRefresh(context.Context, RefreshAttemptResult, time.Duration) {
}

type Refresher struct {
	repositories RefreshStore
	fetcher      Fetcher
	placements   placement.Set
	interval     time.Duration
	fetchTimeout time.Duration
	logger       *slog.Logger
	newID        func() uuid.UUID
	now          func() time.Time
	observer     RefreshObserver
}

func NewRefresher(
	repositories RefreshStore,
	fetcher Fetcher,
	placements placement.Set,
	interval time.Duration,
	fetchTimeout time.Duration,
	logger *slog.Logger,
	options ...RefresherOption,
) *Refresher {
	refresher := &Refresher{
		repositories: repositories,
		fetcher:      fetcher,
		placements:   placements,
		interval:     interval,
		fetchTimeout: fetchTimeout,
		logger:       logger.With("component", "repository_refresher"),
		newID:        uuid.New,
		now:          time.Now,
		observer:     noopRefreshObserver{},
	}
	for _, option := range options {
		option(refresher)
	}

	return refresher
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
	startedAt := time.Now()
	attemptedAt := r.now().UTC()
	fetchContext, cancel := context.WithTimeout(ctx, r.fetchTimeout)
	fetched, fetchErr := r.fetcher.Fetch(fetchContext, item)
	cancel()
	if fetchErr == nil {
		defer func() {
			if err := fetched.Close(); err != nil {
				r.logger.Warn(
					"clean repository refresh directory",
					"repository", item.Name,
					"error", err,
				)
			}
		}()
	}

	if ctx.Err() != nil {
		return
	}

	result := RefreshResult{AttemptedAt: attemptedAt}
	if fetchErr == nil {
		result.Commit = &fetched.Commit

		validation, loadErr := catalog.Process(fetched.Files, catalog.Options{Placements: &r.placements})
		if loadErr == nil && !validation.Diagnostics.HasErrors() {
			result.ChangedPaths = append([]string(nil), fetched.ChangedPaths...)
			result.Revision = &catalog.Revision{
				ID:        r.newID(),
				CommitSHA: fetched.Commit.SHA,
				CreatedAt: attemptedAt,
				Catalog:   validation.Catalog,
			}
		} else {
			failureReason := ""
			if loadErr != nil {
				failureReason = loadErr.Error()
			} else {
				failureReason = validation.Diagnostics.ErrorSummary()
			}
			result.FailureReason = &failureReason
		}
	} else {
		failureReason := fetchErr.Error()
		result.FailureReason = &failureReason
	}

	err := r.repositories.RecordRepositoryRefresh(ctx, item.ID, result)
	if err != nil {
		if ctx.Err() != nil {
			return
		}

		r.observer.ObserveRepositoryRefresh(ctx, RefreshAttemptPersistFailed, time.Since(startedAt))
		if errors.Is(err, ErrNotFound) {
			return
		}

		r.logger.Error(
			"record repository refresh",
			"repository", item.Name,
			"error", err,
		)

		return
	}

	if fetchErr != nil {
		r.observer.ObserveRepositoryRefresh(ctx, RefreshAttemptFetchFailed, time.Since(startedAt))
		r.logger.Warn(
			"repository refresh failed",
			"repository", item.Name,
			"error", fetchErr,
		)

		return
	}
	if result.FailureReason != nil {
		r.observer.ObserveRepositoryRefresh(ctx, RefreshAttemptCatalogRejected, time.Since(startedAt))
		r.logger.Warn(
			"repository catalog rejected",
			"repository", item.Name,
			"commit", fetched.Commit.SHA,
			"error", *result.FailureReason,
		)

		return
	}

	r.observer.ObserveRepositoryRefresh(ctx, RefreshAttemptSucceeded, time.Since(startedAt))
	r.logger.Info(
		"repository catalog indexed",
		"repository", item.Name,
		"commit", fetched.Commit.SHA,
	)
}

func ValidateRefreshResult(result RefreshResult) error {
	if result.AttemptedAt.IsZero() {
		return errors.New("refresh attempted time is required")
	}

	if result.Commit == nil && result.FailureReason == nil {
		return errors.New("refresh result must contain a commit or a failure")
	}

	if result.Commit != nil && !ValidCommitSHA(result.Commit.SHA) {
		return fmt.Errorf("invalid fetched Git commit %q", result.Commit.SHA)
	}

	if result.FailureReason != nil && *result.FailureReason == "" {
		return errors.New("refresh failure reason must not be empty")
	}
	if result.Revision == nil {
		if result.FailureReason == nil {
			return errors.New("successful refresh must contain a catalog revision")
		}

		return nil
	}

	if result.FailureReason != nil {
		return errors.New("failed refresh must not contain a catalog revision")
	}
	if result.Commit == nil {
		return errors.New("catalog revision requires a fetched commit")
	}
	if result.Revision.ID == uuid.Nil {
		return errors.New("catalog revision ID is required")
	}
	if result.Revision.CommitSHA != result.Commit.SHA {
		return errors.New("catalog revision commit must match fetched commit")
	}
	if !result.Revision.CreatedAt.Equal(result.AttemptedAt) {
		return errors.New("catalog revision time must match refresh attempt")
	}

	return nil
}
