package repository

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRefresherRecordsSuccessesAndFailures(t *testing.T) {
	t.Parallel()

	succeededID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	failedID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	succeededCfg := Config{Name: "applications"}
	failedCfg := Config{Name: "infrastructure"}
	repositories := &recordingRefreshStore{
		items: []Repository{
			{ID: succeededID, Config: succeededCfg},
			{ID: failedID, Config: failedCfg},
		},
	}
	fetcher := &recordingFetcher{
		commits: map[RepositoryID]string{
			succeededID: "0123456789abcdef0123456789abcdef01234567",
		},
		errors: map[RepositoryID]error{
			failedID: errors.New("remote branch does not exist"),
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refresher := NewRefresher(
		repositories,
		fetcher,
		time.Minute,
		10*time.Second,
		logger,
	)
	attemptedAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	refresher.now = func() time.Time { return attemptedAt }

	refresher.refreshAll(context.Background())

	if len(repositories.results) != 2 {
		t.Fatalf("recorded results = %d, want 2", len(repositories.results))
	}

	succeeded := repositories.results[succeededID]
	if succeeded.CommitSHA == nil ||
		*succeeded.CommitSHA != fetcher.commits[succeededID] ||
		succeeded.FailureReason != nil ||
		!succeeded.AttemptedAt.Equal(attemptedAt) {
		t.Errorf("successful refresh = %#v", succeeded)
	}

	failed := repositories.results[failedID]
	if failed.CommitSHA != nil || failed.FailureReason == nil ||
		*failed.FailureReason != "remote branch does not exist" ||
		!failed.AttemptedAt.Equal(attemptedAt) {
		t.Errorf("failed refresh = %#v", failed)
	}
}

func TestRefresherBoundsEachFetch(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	cfg := Config{Name: "infrastructure"}
	repositories := &recordingRefreshStore{
		items: []Repository{{
			ID:     repositoryID,
			Config: cfg,
		}},
	}
	fetcher := blockingFetcher{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refresher := NewRefresher(
		repositories,
		fetcher,
		time.Minute,
		10*time.Millisecond,
		logger,
	)

	refresher.refreshAll(context.Background())

	result := repositories.results[repositoryID]
	if result.FailureReason == nil ||
		*result.FailureReason != context.DeadlineExceeded.Error() {
		t.Errorf("timeout result = %#v", result)
	}
}

type recordingRefreshStore struct {
	items   []Repository
	results map[RepositoryID]RefreshResult
}

func (s *recordingRefreshStore) ListRepositories(
	context.Context,
) ([]Repository, error) {
	return s.items, nil
}

func (s *recordingRefreshStore) RecordRepositoryRefresh(
	_ context.Context,
	repositoryID RepositoryID,
	result RefreshResult,
) error {
	if s.results == nil {
		s.results = map[RepositoryID]RefreshResult{}
	}

	s.results[repositoryID] = result

	return nil
}

type recordingFetcher struct {
	commits map[RepositoryID]string
	errors  map[RepositoryID]error
}

func (f *recordingFetcher) Fetch(
	_ context.Context,
	repository Repository,
) (string, error) {
	return f.commits[repository.ID], f.errors[repository.ID]
}

type blockingFetcher struct{}

func (blockingFetcher) Fetch(
	ctx context.Context,
	_ Repository,
) (string, error) {
	<-ctx.Done()

	return "", ctx.Err()
}
