package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Store interface {
	CreateRepository(ctx context.Context, repository Repository) (Repository, error)
	ListRepositories(ctx context.Context) ([]Repository, error)
	GetRepository(ctx context.Context, repositoryID uuid.UUID) (Repository, error)
	ListRepositoryRefreshAttempts(
		ctx context.Context,
		repositoryID uuid.UUID,
		limit int,
		offset int,
	) (RefreshAttemptPage, error)
	UpdateRepository(
		ctx context.Context,
		repositoryID uuid.UUID,
		cfg Config,
	) (Repository, error)
	RemoveRepository(ctx context.Context, repositoryID uuid.UUID, removedAt time.Time) error
}

type RefreshRunner interface {
	RefreshRepository(ctx context.Context, repositoryID RepositoryID) (RefreshAttempt, error)
}

type ConnectionTester interface {
	TestConnection(ctx context.Context, cfg ConnectionConfig) (string, error)
}

type ConnectionTestStatus string

const (
	ConnectionTestSucceeded ConnectionTestStatus = "succeeded"
	ConnectionTestFailed    ConnectionTestStatus = "failed"
)

type ConnectionTestResult struct {
	Status     ConnectionTestStatus
	Duration   time.Duration
	CommitSHA  *string
	Diagnostic *string
}

type ServiceOption func(*Service)

func WithRefresher(refresher RefreshRunner) ServiceOption {
	return func(service *Service) {
		service.refresher = refresher
	}
}

func WithConnectionTester(tester ConnectionTester, timeout time.Duration) ServiceOption {
	return func(service *Service) {
		service.connectionTester = tester
		service.connectionTestTimeout = timeout
	}
}

type Service struct {
	repositories          Store
	credentials           RefreshCredentials
	refresher             RefreshRunner
	connectionTester      ConnectionTester
	connectionTestTimeout time.Duration
	newID                 func() uuid.UUID
	now                   func() time.Time
}

func (s *Service) TestConnection(ctx context.Context, cfg ConnectionConfig) (ConnectionTestResult, error) {
	if err := cfg.Validate(); err != nil {
		return ConnectionTestResult{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if err := s.credentials.validateReference(cfg.RefreshCredentialName); err != nil {
		return ConnectionTestResult{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if s.connectionTester == nil || s.connectionTestTimeout <= 0 {
		return ConnectionTestResult{}, errors.New("repository connection tester is unavailable")
	}

	startedAt := time.Now()
	testContext, cancel := context.WithTimeout(ctx, s.connectionTestTimeout)
	commitSHA, err := s.connectionTester.TestConnection(testContext, cfg)
	cancel()
	duration := time.Since(startedAt)
	if ctx.Err() != nil {
		return ConnectionTestResult{}, ctx.Err()
	}
	if err != nil {
		diagnostic := connectionTestDiagnostic(err)

		return ConnectionTestResult{
			Status:     ConnectionTestFailed,
			Duration:   duration,
			Diagnostic: &diagnostic,
		}, nil
	}

	return ConnectionTestResult{
		Status:    ConnectionTestSucceeded,
		Duration:  duration,
		CommitSHA: &commitSHA,
	}, nil
}

func connectionTestDiagnostic(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "The repository connection test timed out."
	case errors.Is(err, ErrBranchNotFound):
		return "The tracked branch was not found."
	case errors.Is(err, ErrInvalidGitResponse):
		return "Git returned an invalid tracked branch head."
	default:
		return "Git could not reach the tracked branch with the selected refresh credential."
	}
}

func NewService(repositories Store, credentials RefreshCredentials, options ...ServiceOption) *Service {
	service := &Service{
		repositories: repositories,
		credentials:  credentials,
		newID:        uuid.New,
		now:          time.Now,
	}
	for _, option := range options {
		option(service)
	}

	return service
}

func (s *Service) RequestRefresh(ctx context.Context, repositoryID RepositoryID) (RefreshAttempt, error) {
	if s.refresher == nil {
		return RefreshAttempt{}, errors.New("repository refresher is unavailable")
	}

	return s.refresher.RefreshRepository(ctx, repositoryID)
}

func (s *Service) ListRefreshAttempts(
	ctx context.Context,
	repositoryID RepositoryID,
	limit int,
	offset int,
) (RefreshAttemptPage, error) {
	if _, err := s.repositories.GetRepository(ctx, repositoryID); err != nil {
		return RefreshAttemptPage{}, err
	}

	return s.repositories.ListRepositoryRefreshAttempts(ctx, repositoryID, limit, offset)
}

func (s *Service) CreateRepository(ctx context.Context, cfg Config) (Repository, error) {
	if err := cfg.Validate(); err != nil {
		return Repository{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	if err := s.credentials.validateReference(cfg.RefreshCredentialName); err != nil {
		return Repository{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
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

func (s *Service) ListRepositories(ctx context.Context) ([]Repository, error) {
	return s.repositories.ListRepositories(ctx)
}

func (s *Service) GetRepository(ctx context.Context, repositoryID uuid.UUID) (Repository, error) {
	return s.repositories.GetRepository(ctx, repositoryID)
}

func (s *Service) UpdateRepository(
	ctx context.Context,
	repositoryID uuid.UUID,
	update Config,
) (Repository, error) {
	current, err := s.repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		return Repository{}, err
	}

	update.Name = current.Name
	update.CloneURL = current.CloneURL
	update.Branch = current.Branch
	if err := update.Validate(); err != nil {
		return Repository{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if err := s.credentials.validateReference(update.RefreshCredentialName); err != nil {
		return Repository{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	return s.repositories.UpdateRepository(ctx, repositoryID, update)
}

func (s *Service) RemoveRepository(ctx context.Context, repositoryID uuid.UUID) error {
	return s.repositories.RemoveRepository(ctx, repositoryID, s.now().UTC())
}
