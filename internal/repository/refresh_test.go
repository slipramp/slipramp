package repository

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/testcheck"
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
	cleanupCount := 0
	changedPaths := []string{"infrastructure/network/main.tf"}
	fetcher := &recordingFetcher{
		commits: map[RepositoryID]FetchedCommit{
			succeededID: {
				CommitSHA:    "0123456789abcdef0123456789abcdef01234567",
				ChangedPaths: changedPaths,
				Files:        validCatalogFiles(),
				cleanup: func() error {
					cleanupCount++

					return nil
				},
			},
		},
		errors: map[RepositoryID]error{
			failedID: errors.New("remote branch does not exist"),
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refresher := NewRefresher(
		repositories,
		fetcher,
		testRepositoryPlacements(t),
		time.Minute,
		10*time.Second,
		logger,
	)
	attemptedAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	refresher.now = func() time.Time { return attemptedAt }
	refresher.newID = func() uuid.UUID { return revisionID }

	refresher.refreshAll(context.Background())

	if len(repositories.results) != 2 {
		t.Fatalf("recorded results = %d, want 2", len(repositories.results))
	}

	succeeded := repositories.results[succeededID]
	if succeeded.CommitSHA == nil ||
		*succeeded.CommitSHA != fetcher.commits[succeededID].CommitSHA ||
		succeeded.FailureReason != nil ||
		!succeeded.AttemptedAt.Equal(attemptedAt) ||
		succeeded.Revision == nil {
		t.Errorf("successful refresh = %#v", succeeded)
	}
	if strings.Join(succeeded.ChangedPaths, "\n") != strings.Join(changedPaths, "\n") {
		t.Errorf("successful refresh changed paths = %#v", succeeded.ChangedPaths)
	}
	if succeeded.Revision != nil &&
		(succeeded.Revision.ID != revisionID ||
			succeeded.Revision.CommitSHA != *succeeded.CommitSHA ||
			!succeeded.Revision.CreatedAt.Equal(attemptedAt) ||
			len(succeeded.Revision.Catalog.Runtimes) != 1 ||
			len(succeeded.Revision.Catalog.Targets) != 1) {
		t.Errorf("catalog revision = %#v", succeeded.Revision)
	}
	if cleanupCount != 1 {
		t.Errorf("fetched commit cleanup count = %d, want 1", cleanupCount)
	}

	failed := repositories.results[failedID]
	if failed.CommitSHA != nil || failed.FailureReason == nil ||
		*failed.FailureReason != "remote branch does not exist" ||
		!failed.AttemptedAt.Equal(attemptedAt) {
		t.Errorf("failed refresh = %#v", failed)
	}
}

func TestRefresherRecordsCatalogValidationFailure(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	repositories := &recordingRefreshStore{
		items: []Repository{{
			ID:   repositoryID,
			Name: "infrastructure",
		}},
	}
	fetcher := &recordingFetcher{
		commits: map[RepositoryID]FetchedCommit{
			repositoryID: {
				CommitSHA: commitSHA,
				Files: fstest.MapFS{
					".slipramp/catalog.yaml": {
						Data: []byte("apiVersion: ["),
					},
				},
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refresher := NewRefresher(
		repositories,
		fetcher,
		testRepositoryPlacements(t),
		time.Minute,
		10*time.Second,
		logger,
	)

	refresher.refreshAll(context.Background())

	result := repositories.results[repositoryID]
	if result.CommitSHA == nil || *result.CommitSHA != commitSHA ||
		result.Revision != nil || result.FailureReason == nil ||
		!strings.Contains(*result.FailureReason, ".slipramp/catalog.yaml") {
		t.Errorf("invalid catalog refresh = %#v", result)
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
		placement.Set{},
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

func TestValidateRefreshResult(t *testing.T) {
	t.Parallel()

	attemptedAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	failureReason := "catalog is invalid"
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	validRevision := &catalog.Revision{
		ID:        revisionID,
		CommitSHA: commitSHA,
		CreatedAt: attemptedAt,
	}

	tests := []struct {
		name      string
		result    RefreshResult
		wantError string
	}{
		{
			name: "accepted catalog",
			result: RefreshResult{
				CommitSHA:   &commitSHA,
				AttemptedAt: attemptedAt,
				Revision:    validRevision,
			},
		},
		{
			name: "fetch failure",
			result: RefreshResult{
				AttemptedAt:   attemptedAt,
				FailureReason: &failureReason,
			},
		},
		{
			name: "catalog rejection",
			result: RefreshResult{
				CommitSHA:     &commitSHA,
				AttemptedAt:   attemptedAt,
				FailureReason: &failureReason,
			},
		},
		{
			name: "success without revision",
			result: RefreshResult{
				CommitSHA:   &commitSHA,
				AttemptedAt: attemptedAt,
			},
			wantError: "catalog revision",
		},
		{
			name: "failure with revision",
			result: RefreshResult{
				CommitSHA:     &commitSHA,
				AttemptedAt:   attemptedAt,
				FailureReason: &failureReason,
				Revision:      validRevision,
			},
			wantError: "failed refresh",
		},
		{
			name: "mismatched revision commit",
			result: RefreshResult{
				CommitSHA:   &commitSHA,
				AttemptedAt: attemptedAt,
				Revision: &catalog.Revision{
					ID:        revisionID,
					CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					CreatedAt: attemptedAt,
				},
			},
			wantError: "must match",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			testcheck.ErrorContains(
				t,
				ValidateRefreshResult(test.result),
				test.wantError,
			)
		})
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
	commits map[RepositoryID]FetchedCommit
	errors  map[RepositoryID]error
}

func (f *recordingFetcher) Fetch(
	_ context.Context,
	repository Repository,
) (FetchedCommit, error) {
	return f.commits[repository.ID], f.errors[repository.ID]
}

type blockingFetcher struct{}

func (blockingFetcher) Fetch(
	ctx context.Context,
	_ Repository,
) (FetchedCommit, error) {
	<-ctx.Done()

	return FetchedCommit{}, ctx.Err()
}

func validCatalogFiles() fstest.MapFS {
	return fstest.MapFS{
		".slipramp/catalog.yaml": {Data: []byte(`
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: tools
spec:
  image: example.com/tools:1
---
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: infrastructure
spec:
  runtime: tools
  placement: test
  actions:
    inspect:
      command: [inspect]
`)},
	}
}

func testRepositoryPlacements(t *testing.T) placement.Set {
	t.Helper()

	configured, err := placement.NewSet(map[string]placement.Config{
		"test": {
			ClusterName:  "test",
			JobNamespace: "test",
		},
	})
	if err != nil {
		t.Fatalf("placement.NewSet() error = %v", err)
	}

	return configured
}
