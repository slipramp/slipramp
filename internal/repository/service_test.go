package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestServiceCreatesFrozenRepository(t *testing.T) {
	t.Parallel()

	repositories := &recordingStore{}
	service := NewService(repositories, RefreshCredentials{"github-server": "/credentials/github.gitconfig"})
	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	createdAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	service.newID = func() uuid.UUID { return repositoryID }
	service.now = func() time.Time { return createdAt }
	refreshCredentialName := "github-server"
	checkoutCredentialName := "github-readonly"
	cfg := validRepositoryConfig()
	cfg.CloneURL = "git@github.com:example/infrastructure.git"
	cfg.RefreshCredentialName = &refreshCredentialName
	cfg.CheckoutCredentialName = &checkoutCredentialName

	created, err := service.CreateRepository(context.Background(), cfg)
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	if created.ID != repositoryID || created.CreatedAt != createdAt {
		t.Errorf("created identity = (%s, %s), want (%s, %s)", created.ID, created.CreatedAt, repositoryID, createdAt)
	}

	if repositories.created.Name != "infrastructure" ||
		repositories.created.RefreshCredentialName == nil ||
		*repositories.created.RefreshCredentialName != refreshCredentialName ||
		repositories.created.CheckoutCredentialName == nil ||
		*repositories.created.CheckoutCredentialName != checkoutCredentialName {
		t.Errorf("created repository = %#v", repositories.created)
	}
}

func TestServiceRejectsInvalidConfigBeforePersistence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configure   func(*Config)
		credentials RefreshCredentials
		wantCauses  []error
	}{
		{
			name: "invalid repository configuration",
			configure: func(cfg *Config) {
				cfg.CloneURL = "https://token@example.com/infrastructure.git"
			},
			wantCauses: []error{ErrInvalidConfig},
		},
		{
			name: "unknown refresh credential",
			configure: func(cfg *Config) {
				credentialName := "github-server"
				cfg.RefreshCredentialName = &credentialName
			},
			wantCauses: []error{ErrInvalidConfig, ErrUnknownRefreshCredential},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repositories := &recordingStore{}
			service := NewService(repositories, test.credentials)
			cfg := validRepositoryConfig()
			test.configure(&cfg)

			_, err := service.CreateRepository(context.Background(), cfg)
			for _, cause := range test.wantCauses {
				if !errors.Is(err, cause) {
					t.Errorf("CreateRepository() error = %v, want %v", err, cause)
				}
			}

			if repositories.createCalls != 0 {
				t.Errorf("persistence calls = %d, want 0", repositories.createCalls)
			}
		})
	}
}

func TestServiceUpdatesCommitIndexing(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.New()
	repositories := &recordingStore{}
	service := NewService(repositories, nil)

	updated, err := service.UpdateRepositoryCommitIndexing(
		context.Background(),
		repositoryID,
		CommitIndexingTrackedBranchHistory,
	)
	if err != nil {
		t.Fatalf("UpdateRepositoryCommitIndexing() error = %v", err)
	}
	if repositories.updatedID != repositoryID ||
		repositories.updatedMode != CommitIndexingTrackedBranchHistory ||
		updated.CommitIndexing != CommitIndexingTrackedBranchHistory {
		t.Errorf("updated repository = %#v", updated)
	}

	_, err = service.UpdateRepositoryCommitIndexing(context.Background(), repositoryID, "everything")
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("invalid UpdateRepositoryCommitIndexing() error = %v, want %v", err, ErrInvalidConfig)
	}
}

func TestServiceRequestsAndListsRefreshAttempts(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.New()
	attempt := RefreshAttempt{ID: uuid.New(), RepositoryID: repositoryID, Source: RefreshSourceRequested}
	repositories := &recordingStore{attempts: RefreshAttemptPage{Items: []RefreshAttempt{attempt}, Total: 1}}
	refresher := &recordingRefreshRunner{attempt: attempt}
	service := NewService(repositories, nil, WithRefresher(refresher))

	requested, err := service.RequestRefresh(t.Context(), repositoryID)
	if err != nil {
		t.Fatalf("RequestRefresh() error = %v", err)
	}
	if requested.ID != attempt.ID || refresher.repositoryID != repositoryID {
		t.Errorf("requested attempt = %#v, repository ID = %s", requested, refresher.repositoryID)
	}

	page, err := service.ListRefreshAttempts(t.Context(), repositoryID, 25, 0)
	if err != nil {
		t.Fatalf("ListRefreshAttempts() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != attempt.ID {
		t.Errorf("refresh attempt page = %#v", page)
	}
}

type recordingStore struct {
	created     Repository
	createCalls int
	updatedID   uuid.UUID
	updatedMode CommitIndexingMode
	attempts    RefreshAttemptPage
}

func (s *recordingStore) CreateRepository(_ context.Context, repository Repository) (Repository, error) {
	s.createCalls++
	s.created = repository

	return repository, nil
}

func (*recordingStore) ListRepositories(context.Context) ([]Repository, error) {
	return nil, nil
}

func (*recordingStore) GetRepository(context.Context, uuid.UUID) (Repository, error) {
	return Repository{}, nil
}

func (s *recordingStore) ListRepositoryRefreshAttempts(
	context.Context,
	uuid.UUID,
	int,
	int,
) (RefreshAttemptPage, error) {
	return s.attempts, nil
}

func (s *recordingStore) UpdateRepositoryCommitIndexing(
	_ context.Context,
	repositoryID uuid.UUID,
	mode CommitIndexingMode,
) (Repository, error) {
	s.updatedID = repositoryID
	s.updatedMode = mode

	config := Config{CommitIndexing: mode}

	return Repository{Config: config}, nil
}

func (*recordingStore) RemoveRepository(context.Context, uuid.UUID, time.Time) error {
	return nil
}

type recordingRefreshRunner struct {
	repositoryID uuid.UUID
	attempt      RefreshAttempt
}

func (r *recordingRefreshRunner) RefreshRepository(
	_ context.Context,
	repositoryID RepositoryID,
) (RefreshAttempt, error) {
	r.repositoryID = repositoryID

	return r.attempt, nil
}
