package store

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

func TestRepositoryCommitHistoryPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.New()
	baseTime := time.Now().UTC().Truncate(time.Second)
	created, err := database.CreateRepository(ctx, repository.Repository{
		Name:           "commit-history",
		CloneURL:       "https://github.com/example/commit-history.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingTrackedBranchHistory,
		ID:             repositoryID,
		CreatedAt:      baseTime,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if created.CommitHistoryIndex == nil ||
		created.CommitHistoryIndex.Status != repository.CommitHistoryIndexPending {
		t.Fatalf("initial commit history index = %#v", created.CommitHistoryIndex)
	}

	commitA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	commitB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	commitC := "cccccccccccccccccccccccccccccccccccccccc"
	commitD := "dddddddddddddddddddddddddddddddddddddddd"
	commitE := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		successfulCatalogRefresh(commitB, baseTime.Add(time.Minute), uuid.New()),
	); err != nil {
		t.Fatalf("RecordRepositoryRefresh() error = %v", err)
	}

	assertCommitHistoryState(
		t,
		database,
		repositoryID,
		repository.CommitHistoryIndexPending,
		commitB,
		"",
		0,
	)

	initialResult := repository.CommitHistoryResult{
		HeadSHA: commitB,
		Commits: []repository.Commit{
			commitHistoryRecord(repositoryID, commitB, "second", baseTime),
			commitHistoryRecord(repositoryID, commitA, "first", baseTime.Add(-time.Minute)),
		},
		Replace:     true,
		AttemptedAt: baseTime.Add(2 * time.Minute),
	}
	if err := database.RecordCommitHistory(ctx, repositoryID, initialResult); err != nil {
		t.Fatalf("RecordCommitHistory() initial error = %v", err)
	}
	assertCommitHistoryState(
		t,
		database,
		repositoryID,
		repository.CommitHistoryIndexSucceeded,
		commitB,
		commitB,
		2,
	)
	page, err := database.ListCommitHistory(ctx, repository.CommitHistoryFilter{
		RepositoryID: &repositoryID,
		Limit:        1,
	})
	if err != nil {
		t.Fatalf("ListCommitHistory() error = %v", err)
	}
	if page.Total != 2 || len(page.Commits) != 1 || page.Commits[0].Commit.SHA != commitB ||
		page.Commits[0].RepositoryName != "commit-history" {
		t.Errorf("commit history page = %#v", page)
	}
	loadedCommit, err := database.GetCommit(ctx, repository.CommitKey{
		RepositoryID: repositoryID,
		SHA:          commitB,
	})
	if err != nil {
		t.Fatalf("GetCommit() error = %v", err)
	}
	if loadedCommit.RepositoryName != "commit-history" || loadedCommit.Commit.SHA != commitB ||
		loadedCommit.Commit.Title == nil || *loadedCommit.Commit.Title != "second" {
		t.Errorf("GetCommit() = %#v", loadedCommit)
	}
	_, err = database.GetCommit(ctx, repository.CommitKey{
		RepositoryID: repositoryID,
		SHA:          "ffffffffffffffffffffffffffffffffffffffff",
	})
	if !errors.Is(err, repository.ErrCommitNotFound) {
		t.Fatalf("unknown GetCommit() error = %v, want commit not found", err)
	}
	query := "first"
	page, err = database.ListCommitHistory(ctx, repository.CommitHistoryFilter{
		RepositoryID: &repositoryID,
		Query:        &query,
		Limit:        25,
	})
	if err != nil {
		t.Fatalf("ListCommitHistory() search error = %v", err)
	}
	if page.Total != 1 || len(page.Commits) != 1 || page.Commits[0].Commit.SHA != commitA {
		t.Errorf("searched commit history page = %#v", page)
	}

	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		successfulCatalogRefresh(commitC, baseTime.Add(3*time.Minute), uuid.New()),
	); err != nil {
		t.Fatalf("RecordRepositoryRefresh() incremental error = %v", err)
	}
	if err := database.RecordCommitHistory(ctx, repositoryID, repository.CommitHistoryResult{
		HeadSHA: commitC,
		Commits: []repository.Commit{
			commitHistoryRecord(repositoryID, commitC, "third", baseTime.Add(time.Minute)),
		},
		AttemptedAt: baseTime.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("RecordCommitHistory() incremental error = %v", err)
	}
	assertCommitHistoryState(
		t,
		database,
		repositoryID,
		repository.CommitHistoryIndexSucceeded,
		commitC,
		commitC,
		3,
	)

	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		successfulCatalogRefresh(commitD, baseTime.Add(5*time.Minute), uuid.New()),
	); err != nil {
		t.Fatalf("RecordRepositoryRefresh() rewrite error = %v", err)
	}
	if err := database.RecordCommitHistory(ctx, repositoryID, repository.CommitHistoryResult{
		HeadSHA: commitD,
		Commits: []repository.Commit{
			commitHistoryRecord(repositoryID, commitD, "rewritten", baseTime.Add(2*time.Minute)),
		},
		Replace:     true,
		AttemptedAt: baseTime.Add(6 * time.Minute),
	}); err != nil {
		t.Fatalf("RecordCommitHistory() rewrite error = %v", err)
	}
	assertCommitHistoryState(
		t,
		database,
		repositoryID,
		repository.CommitHistoryIndexSucceeded,
		commitD,
		commitD,
		1,
	)

	failureReason := "catalog rejected"
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, repository.RefreshResult{
		AttemptID:     uuid.New(),
		Source:        repository.RefreshSourcePeriodic,
		Commit:        &repository.Commit{SHA: commitE},
		AttemptedAt:   baseTime.Add(7 * time.Minute),
		FailureReason: &failureReason,
	}); err != nil {
		t.Fatalf("RecordRepositoryRefresh() rejected head error = %v", err)
	}
	if err := database.RecordCommitHistoryFailure(
		ctx,
		repositoryID,
		commitE,
		baseTime.Add(8*time.Minute),
		"remote unavailable",
	); err != nil {
		t.Fatalf("RecordCommitHistoryFailure() error = %v", err)
	}
	loaded, err := database.GetRepository(ctx, repositoryID)
	if err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if loaded.CommitHistoryIndex == nil ||
		loaded.CommitHistoryIndex.Status != repository.CommitHistoryIndexFailed ||
		loaded.CommitHistoryIndex.FailureReason == nil ||
		*loaded.CommitHistoryIndex.FailureReason != "remote unavailable" ||
		loaded.CommitHistoryIndex.CommitCount != 1 {
		t.Errorf("failed commit history index = %#v", loaded.CommitHistoryIndex)
	}

	_, err = database.UpdateRepository(
		ctx,
		repositoryID,
		repository.Config{CommitIndexing: repository.CommitIndexingDisabled},
	)
	if err != nil {
		t.Fatalf("disable commit indexing: %v", err)
	}
	err = database.RecordCommitHistory(ctx, repositoryID, initialResult)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("disabled RecordCommitHistory() error = %v", err)
	}
	page, err = database.ListCommitHistory(ctx, repository.CommitHistoryFilter{Limit: 25})
	if err != nil {
		t.Fatalf("disabled ListCommitHistory() error = %v", err)
	}
	if page.Total != 0 || len(page.Commits) != 0 {
		t.Errorf("disabled commit history page = %#v", page)
	}
}

func commitHistoryRecord(
	repositoryID repository.RepositoryID,
	sha string,
	title string,
	timestamp time.Time,
) repository.Commit {
	author := "Example Operator"

	return repository.Commit{
		RepositoryID: repositoryID,
		SHA:          sha,
		Title:        &title,
		AuthorName:   &author,
		AuthoredAt:   &timestamp,
		CommittedAt:  &timestamp,
	}
}

func assertCommitHistoryState(
	t *testing.T,
	database *Database,
	repositoryID repository.RepositoryID,
	wantStatus repository.CommitHistoryIndexStatus,
	wantDesiredHead string,
	wantIndexedHead string,
	wantCount int,
) {
	t.Helper()

	loaded, err := database.GetRepository(t.Context(), repositoryID)
	if err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	index := loaded.CommitHistoryIndex
	if index == nil || index.Status != wantStatus ||
		index.DesiredHeadSHA == nil || *index.DesiredHeadSHA != wantDesiredHead ||
		index.CommitCount != wantCount {
		t.Fatalf("commit history index = %#v", index)
	}
	if wantIndexedHead == "" {
		if index.IndexedHeadSHA != nil {
			t.Errorf("indexed head = %q, want nil", *index.IndexedHeadSHA)
		}
	} else if index.IndexedHeadSHA == nil || *index.IndexedHeadSHA != wantIndexedHead {
		t.Errorf("indexed head = %v, want %q", index.IndexedHeadSHA, wantIndexedHead)
	}
}
