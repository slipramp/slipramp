package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

const (
	MaximumRefreshDiagnosticLength = 4096
	RefreshAttemptRetentionLimit   = 50
)

type RefreshSource string

const (
	RefreshSourcePeriodic  RefreshSource = "periodic"
	RefreshSourceRequested RefreshSource = "requested"
)

type RefreshOutcome string

const (
	RefreshOutcomeAccepted        RefreshOutcome = "accepted"
	RefreshOutcomeFetchFailed     RefreshOutcome = "fetch-failed"
	RefreshOutcomeCatalogRejected RefreshOutcome = "catalog-rejected"
)

type RefreshAttempt struct {
	ID           uuid.UUID
	RepositoryID RepositoryID
	Source       RefreshSource
	Outcome      RefreshOutcome
	StartedAt    time.Time
	Duration     time.Duration
	CommitSHA    *string
	Diagnostic   *string
}

type RefreshAttemptPage struct {
	Items []RefreshAttempt
	Total int
}

type RefreshResult struct {
	AttemptID     uuid.UUID
	Source        RefreshSource
	Commit        *Commit
	ChangedPaths  []string
	AttemptedAt   time.Time
	Duration      time.Duration
	FailureReason *string
	Revision      *catalog.Revision
}

type RefreshStore interface {
	ListRepositories(ctx context.Context) ([]Repository, error)
	GetRepository(ctx context.Context, repositoryID uuid.UUID) (Repository, error)
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
	locks        sync.Map
}

type refreshSemaphore chan struct{}

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

		_, _ = r.refresh(ctx, item, RefreshSourcePeriodic)
	}
}

func (r *Refresher) RefreshRepository(ctx context.Context, repositoryID RepositoryID) (RefreshAttempt, error) {
	item, err := r.repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		return RefreshAttempt{}, err
	}

	return r.refresh(ctx, item, RefreshSourceRequested)
}

func (r *Refresher) refresh(
	ctx context.Context,
	item Repository,
	source RefreshSource,
) (RefreshAttempt, error) {
	unlock, err := r.lockRepository(ctx, item.ID)
	if err != nil {
		return RefreshAttempt{}, err
	}
	defer unlock()

	// A requested refresh may have completed while this attempt waited for the
	// per-Repository lock. Reload the accepted head so changed-path calculation
	// always compares against the latest durable catalog state.
	item, err = r.repositories.GetRepository(ctx, item.ID)
	if err != nil {
		return RefreshAttempt{}, err
	}

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
		return RefreshAttempt{}, ctx.Err()
	}

	result := RefreshResult{
		AttemptID:   r.newID(),
		Source:      source,
		AttemptedAt: attemptedAt,
	}
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
			failureReason := "The fetched repository could not be read as a catalog."
			if loadErr != nil {
				r.logger.Warn(
					"load repository catalog",
					"repository", item.Name,
					"commit", fetched.Commit.SHA,
					"error", loadErr,
				)
			} else {
				failureReason = boundedRefreshDiagnostic(validation.Diagnostics.ErrorSummary())
			}
			result.FailureReason = &failureReason
		}
	} else {
		failureReason := "Git fetch failed. Check repository access, branch, and refresh credentials."
		result.FailureReason = &failureReason
	}
	result.Duration = time.Since(startedAt)

	err = r.repositories.RecordRepositoryRefresh(ctx, item.ID, result)
	if err != nil {
		if ctx.Err() != nil {
			return RefreshAttempt{}, ctx.Err()
		}

		r.observer.ObserveRepositoryRefresh(ctx, RefreshAttemptPersistFailed, time.Since(startedAt))
		if errors.Is(err, ErrNotFound) {
			return RefreshAttempt{}, err
		}

		r.logger.Error(
			"record repository refresh",
			"repository", item.Name,
			"error", err,
		)

		return RefreshAttempt{}, err
	}
	attempt := result.Attempt(item.ID)

	if fetchErr != nil {
		r.observer.ObserveRepositoryRefresh(ctx, RefreshAttemptFetchFailed, time.Since(startedAt))
		r.logger.Warn(
			"repository refresh failed",
			"repository", item.Name,
			"error", fetchErr,
		)

		return attempt, nil
	}
	if result.FailureReason != nil {
		r.observer.ObserveRepositoryRefresh(ctx, RefreshAttemptCatalogRejected, time.Since(startedAt))
		r.logger.Warn(
			"repository catalog rejected",
			"repository", item.Name,
			"commit", fetched.Commit.SHA,
			"error", *result.FailureReason,
		)

		return attempt, nil
	}

	r.observer.ObserveRepositoryRefresh(ctx, RefreshAttemptSucceeded, time.Since(startedAt))
	r.logger.Info(
		"repository catalog indexed",
		"repository", item.Name,
		"commit", fetched.Commit.SHA,
	)

	return attempt, nil
}

func (r *Refresher) lockRepository(ctx context.Context, repositoryID RepositoryID) (func(), error) {
	semaphoreValue, _ := r.locks.LoadOrStore(repositoryID, newRefreshSemaphore())
	semaphore := semaphoreValue.(refreshSemaphore)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-semaphore:
		return func() { semaphore <- struct{}{} }, nil
	}
}

func newRefreshSemaphore() refreshSemaphore {
	semaphore := make(refreshSemaphore, 1)
	semaphore <- struct{}{}

	return semaphore
}

func (r RefreshResult) Attempt(repositoryID RepositoryID) RefreshAttempt {
	outcome := RefreshOutcomeAccepted
	if r.FailureReason != nil {
		if r.Commit == nil {
			outcome = RefreshOutcomeFetchFailed
		} else {
			outcome = RefreshOutcomeCatalogRejected
		}
	}

	return RefreshAttempt{
		ID:           r.AttemptID,
		RepositoryID: repositoryID,
		Source:       r.Source,
		Outcome:      outcome,
		StartedAt:    r.AttemptedAt,
		Duration:     r.Duration,
		CommitSHA:    optionalRefreshCommitSHA(r.Commit),
		Diagnostic:   r.FailureReason,
	}
}

func optionalRefreshCommitSHA(commit *Commit) *string {
	if commit == nil {
		return nil
	}

	return &commit.SHA
}

func boundedRefreshDiagnostic(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "�")
	if len(value) <= MaximumRefreshDiagnosticLength {
		return value
	}

	value = value[:MaximumRefreshDiagnosticLength]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}

	return value
}

func ValidateRefreshResult(result RefreshResult) error {
	if result.AttemptID == uuid.Nil {
		return errors.New("refresh attempt ID is required")
	}
	if result.Source != RefreshSourcePeriodic && result.Source != RefreshSourceRequested {
		return fmt.Errorf("refresh source %q is invalid", result.Source)
	}
	if result.AttemptedAt.IsZero() {
		return errors.New("refresh attempted time is required")
	}
	if result.Duration < 0 {
		return errors.New("refresh duration must not be negative")
	}

	if result.Commit == nil && result.FailureReason == nil {
		return errors.New("refresh result must contain a commit or a failure")
	}

	if result.Commit != nil && !ValidCommitSHA(result.Commit.SHA) {
		return fmt.Errorf("invalid fetched Git commit %q", result.Commit.SHA)
	}

	if result.FailureReason != nil {
		if *result.FailureReason == "" {
			return errors.New("refresh failure reason must not be empty")
		}
		if !utf8.ValidString(*result.FailureReason) || len(*result.FailureReason) > MaximumRefreshDiagnosticLength {
			return fmt.Errorf(
				"refresh failure reason must contain valid UTF-8 and at most %d bytes",
				MaximumRefreshDiagnosticLength,
			)
		}
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
