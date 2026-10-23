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

func TestCommitHistoryIndexerProcessesOnlyOutdatedTrackedHistories(t *testing.T) {
	t.Parallel()

	indexedHead := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	desiredHead := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	repositoryID := uuid.New()
	store := &recordingCommitHistoryStore{
		repositories: []Repository{
			{
				CommitIndexing: CommitIndexingTrackedBranchHistory,
				ID:             repositoryID,
				CommitHistoryIndex: &CommitHistoryIndex{
					Status:         CommitHistoryIndexPending,
					DesiredHeadSHA: &desiredHead,
					IndexedHeadSHA: &indexedHead,
				},
			},
			{
				CommitIndexing: CommitIndexingDisabled,
				ID:             uuid.New(),
			},
			{
				CommitIndexing: CommitIndexingTrackedBranchHistory,
				ID:             uuid.New(),
				CommitHistoryIndex: &CommitHistoryIndex{
					Status:         CommitHistoryIndexSucceeded,
					DesiredHeadSHA: &desiredHead,
					IndexedHeadSHA: &desiredHead,
				},
			},
		},
	}
	title := "new commit"
	author := "Example Operator"
	commitTime := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	fetcher := &recordingCommitHistoryFetcher{
		result: FetchedCommitHistory{
			HeadSHA: desiredHead,
			Commits: []Commit{{
				RepositoryID: repositoryID,
				SHA:          desiredHead,
				Title:        &title,
				AuthorName:   &author,
				AuthoredAt:   &commitTime,
				CommittedAt:  &commitTime,
			}},
		},
	}
	indexer := NewCommitHistoryIndexer(
		store,
		fetcher,
		time.Minute,
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	attemptedAt := commitTime.Add(time.Minute)
	indexer.now = func() time.Time { return attemptedAt }

	indexer.indexAll(context.Background())

	if fetcher.calls != 1 || fetcher.desiredHeadSHA != desiredHead ||
		fetcher.indexedHeadSHA == nil || *fetcher.indexedHeadSHA != indexedHead {
		t.Errorf("fetch = (%d, %q, %v)", fetcher.calls, fetcher.desiredHeadSHA, fetcher.indexedHeadSHA)
	}
	if store.recordedID != repositoryID || store.recorded.HeadSHA != desiredHead ||
		!store.recorded.AttemptedAt.Equal(attemptedAt) {
		t.Errorf("recorded history = (%s, %#v)", store.recordedID, store.recorded)
	}
}

func TestCommitHistoryIndexerRecordsFetchFailure(t *testing.T) {
	t.Parallel()

	desiredHead := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	repositoryID := uuid.New()
	store := &recordingCommitHistoryStore{
		repositories: []Repository{{
			CommitIndexing: CommitIndexingTrackedBranchHistory,
			ID:             repositoryID,
			CommitHistoryIndex: &CommitHistoryIndex{
				Status:         CommitHistoryIndexPending,
				DesiredHeadSHA: &desiredHead,
			},
		}},
	}
	fetcher := &recordingCommitHistoryFetcher{err: errors.New("remote unavailable")}
	indexer := NewCommitHistoryIndexer(
		store,
		fetcher,
		time.Minute,
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	attemptedAt := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	indexer.now = func() time.Time { return attemptedAt }

	indexer.indexAll(context.Background())

	if store.failedID != repositoryID || store.failedHeadSHA != desiredHead ||
		store.failureReason != "remote unavailable" || !store.failedAt.Equal(attemptedAt) {
		t.Errorf(
			"recorded failure = (%s, %q, %s, %q)",
			store.failedID,
			store.failedHeadSHA,
			store.failedAt,
			store.failureReason,
		)
	}
}

type recordingCommitHistoryFetcher struct {
	result         FetchedCommitHistory
	err            error
	calls          int
	desiredHeadSHA string
	indexedHeadSHA *string
}

func (f *recordingCommitHistoryFetcher) FetchCommitHistory(
	_ context.Context,
	_ Repository,
	desiredHeadSHA string,
	indexedHeadSHA *string,
) (FetchedCommitHistory, error) {
	f.calls++
	f.desiredHeadSHA = desiredHeadSHA
	f.indexedHeadSHA = indexedHeadSHA

	return f.result, f.err
}

type recordingCommitHistoryStore struct {
	repositories  []Repository
	recordedID    RepositoryID
	recorded      CommitHistoryResult
	failedID      RepositoryID
	failedHeadSHA string
	failedAt      time.Time
	failureReason string
}

func (s *recordingCommitHistoryStore) ListRepositories(context.Context) ([]Repository, error) {
	return s.repositories, nil
}

func (s *recordingCommitHistoryStore) RecordCommitHistory(
	_ context.Context,
	repositoryID RepositoryID,
	result CommitHistoryResult,
) error {
	s.recordedID = repositoryID
	s.recorded = result

	return nil
}

func (s *recordingCommitHistoryStore) RecordCommitHistoryFailure(
	_ context.Context,
	repositoryID RepositoryID,
	attemptedHeadSHA string,
	attemptedAt time.Time,
	failureReason string,
) error {
	s.failedID = repositoryID
	s.failedHeadSHA = attemptedHeadSHA
	s.failedAt = attemptedAt
	s.failureReason = failureReason

	return nil
}
