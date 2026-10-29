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

func TestServiceTestsConnectionWithoutPersistence(t *testing.T) {
	t.Parallel()

	credentialName := "github-server"
	tester := &recordingConnectionTester{commitSHA: "0123456789abcdef0123456789abcdef01234567"}
	repositories := &recordingStore{}
	service := NewService(
		repositories,
		RefreshCredentials{credentialName: "/credentials/github.gitconfig"},
		WithConnectionTester(tester, time.Second),
	)
	cfg := ConnectionConfig{
		CloneURL:              "https://github.com/example/infrastructure.git",
		Branch:                "main",
		RefreshCredentialName: &credentialName,
	}

	result, err := service.TestConnection(t.Context(), cfg)
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Status != ConnectionTestSucceeded || result.CommitSHA == nil ||
		*result.CommitSHA != tester.commitSHA || tester.cfg.CloneURL != cfg.CloneURL {
		t.Errorf("connection result = %#v, tester config = %#v", result, tester.cfg)
	}
	if repositories.createCalls != 0 || repositories.updateCalls != 0 {
		t.Errorf("persistence calls = create %d, update %d", repositories.createCalls, repositories.updateCalls)
	}
}

func TestServiceReturnsSafeConnectionFailure(t *testing.T) {
	t.Parallel()

	tester := &recordingConnectionTester{err: errors.New("credential-secret-marker")}
	service := NewService(
		&recordingStore{},
		nil,
		WithConnectionTester(tester, time.Second),
	)
	result, err := service.TestConnection(t.Context(), ConnectionConfig{
		CloneURL: "https://github.com/example/infrastructure.git",
		Branch:   "main",
	})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Status != ConnectionTestFailed || result.Diagnostic == nil ||
		*result.Diagnostic != "Git could not reach the tracked branch with the selected refresh credential." {
		t.Errorf("connection result = %#v", result)
	}
}

func TestServiceRejectsUnknownConnectionCredentialBeforeProbe(t *testing.T) {
	t.Parallel()

	credentialName := "missing"
	tester := &recordingConnectionTester{}
	service := NewService(
		&recordingStore{},
		nil,
		WithConnectionTester(tester, time.Second),
	)
	_, err := service.TestConnection(t.Context(), ConnectionConfig{
		CloneURL:              "https://github.com/example/infrastructure.git",
		Branch:                "main",
		RefreshCredentialName: &credentialName,
	})
	if !errors.Is(err, ErrInvalidConfig) || !errors.Is(err, ErrUnknownRefreshCredential) {
		t.Errorf("TestConnection() error = %v", err)
	}
	if tester.calls != 0 {
		t.Errorf("connection probe calls = %d, want 0", tester.calls)
	}
}

func TestServiceUpdatesRepositoryConfiguration(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.New()
	repositories := &recordingStore{current: Repository{Config: validRepositoryConfig()}}
	credentialName := "github-server"
	service := NewService(repositories, RefreshCredentials{credentialName: "/credentials/github.gitconfig"})
	commitURLTemplate := "https://github.com/example/infrastructure/commit/{sha}"
	update := Config{
		CommitIndexing:        CommitIndexingTrackedBranchHistory,
		CommitURLTemplate:     &commitURLTemplate,
		RefreshCredentialName: &credentialName,
	}

	updated, err := service.UpdateRepository(context.Background(), repositoryID, update)
	if err != nil {
		t.Fatalf("UpdateRepository() error = %v", err)
	}
	if repositories.updatedID != repositoryID || repositories.updatedConfig.Name != "infrastructure" ||
		repositories.updatedConfig.RefreshCredentialName == nil ||
		*repositories.updatedConfig.RefreshCredentialName != credentialName ||
		updated.CommitIndexing != CommitIndexingTrackedBranchHistory {
		t.Errorf("updated repository = %#v", updated)
	}

	update.CommitIndexing = "everything"
	_, err = service.UpdateRepository(context.Background(), repositoryID, update)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("invalid UpdateRepository() error = %v, want %v", err, ErrInvalidConfig)
	}
	if repositories.updateCalls != 1 {
		t.Errorf("persistence update calls = %d, want 1", repositories.updateCalls)
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
	created       Repository
	current       Repository
	createCalls   int
	updateCalls   int
	updatedID     uuid.UUID
	updatedConfig Config
	attempts      RefreshAttemptPage
}

func (s *recordingStore) CreateRepository(_ context.Context, repository Repository) (Repository, error) {
	s.createCalls++
	s.created = repository

	return repository, nil
}

func (*recordingStore) ListRepositories(context.Context) ([]Repository, error) {
	return nil, nil
}

func (s *recordingStore) GetRepository(context.Context, uuid.UUID) (Repository, error) {
	return s.current, nil
}

func (s *recordingStore) ListRepositoryRefreshAttempts(
	context.Context,
	uuid.UUID,
	int,
	int,
) (RefreshAttemptPage, error) {
	return s.attempts, nil
}

func (s *recordingStore) UpdateRepository(
	_ context.Context,
	repositoryID uuid.UUID,
	cfg Config,
) (Repository, error) {
	s.updateCalls++
	s.updatedID = repositoryID
	s.updatedConfig = cfg

	return Repository{Config: cfg}, nil
}

func (*recordingStore) RemoveRepository(context.Context, uuid.UUID, time.Time) error {
	return nil
}

type recordingRefreshRunner struct {
	repositoryID uuid.UUID
	attempt      RefreshAttempt
}

type recordingConnectionTester struct {
	cfg       ConnectionConfig
	commitSHA string
	err       error
	calls     int
}

func (t *recordingConnectionTester) TestConnection(
	_ context.Context,
	cfg ConnectionConfig,
) (string, error) {
	t.calls++
	t.cfg = cfg

	return t.commitSHA, t.err
}

func (r *recordingRefreshRunner) RefreshRepository(
	_ context.Context,
	repositoryID RepositoryID,
) (RefreshAttempt, error) {
	r.repositoryID = repositoryID

	return r.attempt, nil
}
