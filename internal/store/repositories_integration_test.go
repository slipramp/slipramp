package store

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

func TestRepositoryPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	refreshCredentialName := "github-server"
	checkoutCredentialName := "github-readonly"
	first := repository.Repository{
		Name:                   "infrastructure",
		CloneURL:               "git@github.com:example/infrastructure.git",
		Branch:                 "main",
		CommitIndexing:         repository.CommitIndexingObservedHeads,
		RefreshCredentialName:  &refreshCredentialName,
		CheckoutCredentialName: &checkoutCredentialName,
		ID:                     uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200"),
		CreatedAt:              createdAt,
	}

	created, err := database.CreateRepository(ctx, first)
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	if created.ID != first.ID || created.Name != first.Name ||
		created.CloneURL != first.CloneURL || created.Branch != first.Branch ||
		created.RefreshCredentialName == nil ||
		*created.RefreshCredentialName != refreshCredentialName ||
		created.CheckoutCredentialName == nil ||
		*created.CheckoutCredentialName != checkoutCredentialName ||
		!created.CreatedAt.Equal(createdAt) {
		t.Errorf("created repository = %#v, want %#v", created, first)
	}

	loaded, err := database.GetRepository(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if loaded.ID != first.ID || loaded.Name != first.Name ||
		loaded.CloneURL != first.CloneURL {
		t.Errorf("loaded repository = %#v, want %#v", loaded, first)
	}

	conflict := first
	conflict.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	_, err = database.CreateRepository(ctx, conflict)
	if !errors.Is(err, repository.ErrNameConflict) {
		t.Errorf("conflicting CreateRepository() error = %v, want %v", err, repository.ErrNameConflict)
	}

	second := repository.Repository{
		Name:           "applications",
		CloneURL:       "https://github.com/example/applications.git",
		Branch:         "production",
		CommitIndexing: repository.CommitIndexingDisabled,
		ID:             uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"),
		CreatedAt:      createdAt.Add(time.Minute),
	}
	if _, err := database.CreateRepository(ctx, second); err != nil {
		t.Fatalf("create second repository: %v", err)
	}

	repositories, err := database.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}

	if len(repositories) != 2 || repositories[0].Name != "applications" ||
		repositories[1].Name != "infrastructure" {
		t.Errorf("repository list = %#v", repositories)
	}

	if repositories[1].Refresh.Status != repository.RefreshStatusPending {
		t.Errorf(
			"initial refresh status = %q, want %q",
			repositories[1].Refresh.Status,
			repository.RefreshStatusPending,
		)
	}

	updated, err := database.UpdateRepositoryCommitIndexing(
		ctx,
		second.ID,
		repository.CommitIndexingTrackedBranchHistory,
	)
	if err != nil {
		t.Fatalf("UpdateRepositoryCommitIndexing() error = %v", err)
	}
	if updated.CommitIndexing != repository.CommitIndexingTrackedBranchHistory {
		t.Errorf("updated commit indexing = %q", updated.CommitIndexing)
	}

	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	succeededAt := createdAt.Add(90 * time.Second)
	commitTitle := "Update network routes"
	commitAuthor := "Example Operator"
	authoredAt := createdAt.Add(-time.Hour)
	refreshResult := successfulCatalogRefresh(
		commitSHA,
		succeededAt,
		uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1210"),
	)
	refreshResult.Commit.Title = &commitTitle
	refreshResult.Commit.AuthorName = &commitAuthor
	refreshResult.Commit.AuthoredAt = &authoredAt
	refreshResult.Commit.CommittedAt = &authoredAt
	if err := database.RecordRepositoryRefresh(
		ctx,
		first.ID,
		refreshResult,
	); err != nil {
		t.Fatalf("record successful repository refresh: %v", err)
	}
	commitKey := repository.CommitKey{RepositoryID: first.ID, SHA: commitSHA}
	commits, err := database.ListCommits(ctx, []repository.CommitKey{commitKey})
	if err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	commit := commits[commitKey]
	if commit.Title == nil || *commit.Title != commitTitle ||
		commit.AuthorName == nil || *commit.AuthorName != commitAuthor ||
		commit.AuthoredAt == nil || !commit.AuthoredAt.Equal(authoredAt) ||
		commit.CommittedAt == nil || !commit.CommittedAt.Equal(authoredAt) ||
		commit.URL == nil || *commit.URL != "https://github.com/example/infrastructure/commit/"+commitSHA {
		t.Errorf("repository commit = %#v", commit)
	}

	failureReason := "remote branch does not exist"
	failedAt := createdAt.Add(105 * time.Second)
	if err := database.RecordRepositoryRefresh(
		ctx,
		first.ID,
		repository.RefreshResult{
			AttemptedAt:   failedAt,
			FailureReason: &failureReason,
		},
	); err != nil {
		t.Fatalf("record failed repository refresh: %v", err)
	}

	repositories, err = database.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("list repositories after refresh: %v", err)
	}

	refresh := repositories[1].Refresh
	if refresh.Status != repository.RefreshStatusFailed ||
		refresh.CommitSHA == nil || *refresh.CommitSHA != commitSHA ||
		refresh.AttemptedAt == nil || !refresh.AttemptedAt.Equal(failedAt) ||
		refresh.SucceededAt == nil || !refresh.SucceededAt.Equal(succeededAt) ||
		refresh.FailureReason == nil || *refresh.FailureReason != failureReason {
		t.Errorf("refresh state after failure = %#v", refresh)
	}
	if repositories[1].ActiveCatalogCommitSHA == nil ||
		*repositories[1].ActiveCatalogCommitSHA != commitSHA {
		t.Errorf("active catalog commit = %v, want %q", repositories[1].ActiveCatalogCommitSHA, commitSHA)
	}

	if err := database.RemoveRepository(ctx, first.ID, createdAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("RemoveRepository() error = %v", err)
	}

	if _, err := database.GetRepository(ctx, first.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("removed GetRepository() error = %v, want %v", err, repository.ErrNotFound)
	}

	if err := database.RecordRepositoryRefresh(
		ctx,
		first.ID,
		successfulCatalogRefresh(
			commitSHA,
			createdAt.Add(150*time.Second),
			uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1211"),
		),
	); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("removed repository refresh error = %v, want %v", err, repository.ErrNotFound)
	}

	if err := database.RemoveRepository(
		ctx,
		first.ID,
		createdAt.Add(3*time.Minute),
	); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("second RemoveRepository() error = %v, want %v", err, repository.ErrNotFound)
	}

	replacement := first
	replacement.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203")
	replacement.CreatedAt = createdAt.Add(4 * time.Minute)
	if _, err := database.CreateRepository(ctx, replacement); err != nil {
		t.Fatalf("reuse removed repository name: %v", err)
	}

	repositories, err = database.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("second ListRepositories() error = %v", err)
	}

	if len(repositories) != 2 || repositories[1].ID != replacement.ID {
		t.Errorf("repository list after replacement = %#v", repositories)
	}
}

func TestDisabledCommitIndexingSkipsRefreshMetadataIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.New()
	createdAt := time.Now().UTC().Truncate(time.Second)
	_, err := database.CreateRepository(ctx, repository.Repository{
		Name:           "metadata-disabled",
		CloneURL:       "https://github.com/example/metadata-disabled.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingDisabled,
		ID:             repositoryID,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	refresh := successfulCatalogRefresh(commitSHA, createdAt.Add(time.Minute), uuid.New())
	title := "metadata that must not be persisted"
	author := "Example Operator"
	authoredAt := createdAt.Add(-time.Hour)
	refresh.Commit.Title = &title
	refresh.Commit.AuthorName = &author
	refresh.Commit.AuthoredAt = &authoredAt
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("RecordRepositoryRefresh() error = %v", err)
	}

	key := repository.CommitKey{RepositoryID: repositoryID, SHA: commitSHA}
	commits, err := database.ListCommits(ctx, []repository.CommitKey{key})
	if err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	commit := commits[key]
	if commit.Title != nil || commit.AuthorName != nil || commit.AuthoredAt != nil {
		t.Errorf("disabled commit metadata = %#v", commit)
	}
}
