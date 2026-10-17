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
	service := NewService(
		repositories,
		RefreshCredentials{"github-server": "/credentials/github.gitconfig"},
	)
	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	createdAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	service.newID = func() uuid.UUID { return repositoryID }
	service.now = func() time.Time { return createdAt }
	refreshCredentialName := "github-server"
	checkoutCredentialName := "github-readonly"

	created, err := service.CreateRepository(
		context.Background(),
		Config{
			Name:                   "infrastructure",
			CloneURL:               "git@github.com:example/infrastructure.git",
			Branch:                 "main",
			RefreshCredentialName:  &refreshCredentialName,
			CheckoutCredentialName: &checkoutCredentialName,
		},
	)
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	if created.ID != repositoryID || created.CreatedAt != createdAt {
		t.Errorf(
			"created identity = (%s, %s), want (%s, %s)",
			created.ID,
			created.CreatedAt,
			repositoryID,
			createdAt,
		)
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

	repositories := &recordingStore{}
	service := NewService(repositories, nil)

	_, err := service.CreateRepository(
		context.Background(),
		Config{
			Name:     "infrastructure",
			CloneURL: "https://token@example.com/infrastructure.git",
			Branch:   "main",
		},
	)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf(
			"CreateRepository() error = %v, want %v",
			err,
			ErrInvalidConfig,
		)
	}

	if repositories.createCalls != 0 {
		t.Errorf("persistence calls = %d, want 0", repositories.createCalls)
	}
}

func TestServiceRejectsUnknownRefreshCredential(t *testing.T) {
	t.Parallel()

	repositories := &recordingStore{}
	service := NewService(repositories, nil)
	credentialName := "github-server"

	_, err := service.CreateRepository(
		context.Background(),
		Config{
			Name:                  "infrastructure",
			CloneURL:              "https://github.com/example/infrastructure.git",
			Branch:                "main",
			RefreshCredentialName: &credentialName,
		},
	)
	if !errors.Is(err, ErrInvalidConfig) ||
		!errors.Is(err, ErrUnknownRefreshCredential) {
		t.Fatalf(
			"CreateRepository() error = %v, want %v and %v",
			err,
			ErrInvalidConfig,
			ErrUnknownRefreshCredential,
		)
	}

	if repositories.createCalls != 0 {
		t.Errorf("persistence calls = %d, want 0", repositories.createCalls)
	}
}

type recordingStore struct {
	created     Repository
	createCalls int
}

func (s *recordingStore) CreateRepository(
	_ context.Context,
	repository Repository,
) (Repository, error) {
	s.createCalls++
	s.created = repository

	return repository, nil
}

func (*recordingStore) ListRepositories(context.Context) ([]Repository, error) {
	return nil, nil
}

func (*recordingStore) RemoveRepository(
	context.Context,
	uuid.UUID,
	time.Time,
) error {
	return nil
}
