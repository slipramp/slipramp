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
		t.Errorf(
			"conflicting CreateRepository() error = %v, want %v",
			err,
			repository.ErrNameConflict,
		)
	}

	second := repository.Repository{
		Name:      "applications",
		CloneURL:  "https://github.com/example/applications.git",
		Branch:    "production",
		ID:        uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"),
		CreatedAt: createdAt.Add(time.Minute),
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

	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	succeededAt := createdAt.Add(90 * time.Second)
	if err := database.RecordRepositoryRefresh(
		ctx,
		first.ID,
		successfulCatalogRefresh(
			commitSHA,
			succeededAt,
			uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1210"),
		),
	); err != nil {
		t.Fatalf("record successful repository refresh: %v", err)
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

	if err := database.RemoveRepository(
		ctx,
		first.ID,
		createdAt.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("RemoveRepository() error = %v", err)
	}

	if _, err := database.GetRepository(ctx, first.ID); !errors.Is(
		err,
		repository.ErrNotFound,
	) {
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
		t.Errorf(
			"removed repository refresh error = %v, want %v",
			err,
			repository.ErrNotFound,
		)
	}

	if err := database.RemoveRepository(
		ctx,
		first.ID,
		createdAt.Add(3*time.Minute),
	); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf(
			"second RemoveRepository() error = %v, want %v",
			err,
			repository.ErrNotFound,
		)
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
