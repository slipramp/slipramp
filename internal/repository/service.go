package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Store interface {
	CreateRepository(
		ctx context.Context,
		repository Repository,
	) (Repository, error)
	ListRepositories(ctx context.Context) ([]Repository, error)
	RemoveRepository(
		ctx context.Context,
		repositoryID uuid.UUID,
		removedAt time.Time,
	) error
}

type Service struct {
	repositories Store
	credentials  RefreshCredentials
	newID        func() uuid.UUID
	now          func() time.Time
}

func NewService(
	repositories Store,
	credentials RefreshCredentials,
) *Service {
	return &Service{
		repositories: repositories,
		credentials:  credentials,
		newID:        uuid.New,
		now:          time.Now,
	}
}

func (s *Service) CreateRepository(
	ctx context.Context,
	cfg Config,
) (Repository, error) {
	if err := cfg.Validate(); err != nil {
		return Repository{}, fmt.Errorf(
			"%w: %w",
			ErrInvalidConfig,
			err,
		)
	}

	if err := s.credentials.validateReference(
		cfg.RefreshCredentialName,
	); err != nil {
		return Repository{}, fmt.Errorf(
			"%w: %w",
			ErrInvalidConfig,
			err,
		)
	}

	record := Repository{
		Config:    cfg,
		ID:        s.newID(),
		CreatedAt: s.now().UTC(),
		Refresh: RefreshState{
			Status: RefreshStatusPending,
		},
	}

	return s.repositories.CreateRepository(ctx, record)
}

func (s *Service) ListRepositories(
	ctx context.Context,
) ([]Repository, error) {
	return s.repositories.ListRepositories(ctx)
}

func (s *Service) RemoveRepository(
	ctx context.Context,
	repositoryID uuid.UUID,
) error {
	return s.repositories.RemoveRepository(
		ctx,
		repositoryID,
		s.now().UTC(),
	)
}
