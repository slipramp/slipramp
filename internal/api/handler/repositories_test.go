package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/repository"
)

func TestCreateRepositoryReturnsRegistration(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	createdAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	repositories := &recordingRepositoryService{
		created: repository.Repository{
			ID:        repositoryID,
			CreatedAt: createdAt,
			Refresh: repository.RefreshState{
				Status: repository.RefreshStatusPending,
			},
		},
	}
	body := bytes.NewBufferString(`{
		"name":"infrastructure",
		"cloneUrl":"https://github.com/example/infrastructure.git",
		"branch":"main",
		"commitIndexing":"trackedBranchHistory",
		"commitUrlTemplate":"https://github.com/example/infrastructure/commit/{sha}",
		"refreshCredentialName":"github-server",
		"checkoutCredentialName":"github-readonly"
	}`)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/repositories", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newAdminTestHandler(&recordingClusterService{}, nil, repositories, nil, nil, nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}

	wantLocation := "/api/v1/repositories/" + repositoryID.String()
	if location := response.Header().Get("Location"); location != wantLocation {
		t.Errorf("Location = %q, want %q", location, wantLocation)
	}

	if repositories.cfg.Name != "infrastructure" ||
		repositories.cfg.CommitIndexing != repository.CommitIndexingTrackedBranchHistory ||
		repositories.cfg.CommitURLTemplate == nil ||
		*repositories.cfg.CommitURLTemplate != "https://github.com/example/infrastructure/commit/{sha}" ||
		repositories.cfg.RefreshCredentialName == nil ||
		*repositories.cfg.RefreshCredentialName != "github-server" ||
		repositories.cfg.CheckoutCredentialName == nil ||
		*repositories.cfg.CheckoutCredentialName != "github-readonly" {
		t.Errorf("cfg = %#v", repositories.cfg)
	}

	var result api.Repository
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.ID != repositoryID ||
		result.CloneURL != "https://github.com/example/infrastructure.git" ||
		result.CommitIndexing != api.RepositoryCommitIndexingTrackedBranchHistory ||
		result.CommitURLTemplate == nil ||
		*result.CommitURLTemplate != "https://github.com/example/infrastructure/commit/{sha}" ||
		result.RefreshCredentialName == nil ||
		*result.RefreshCredentialName != "github-server" ||
		result.CreatedAt != createdAt ||
		result.Refresh.Status != api.RepositoryRefreshStatusPending {
		t.Errorf("repository response = %#v", result)
	}
}

func TestRepositoryHandlersMapExpectedFailures(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	validBody := `{
		"name":"infrastructure",
		"cloneUrl":"https://github.com/example/infrastructure.git",
		"branch":"main",
		"commitIndexing":"observedHeads"
	}`
	tests := []struct {
		name          string
		method        string
		path          string
		body          string
		repositories  *recordingRepositoryService
		wantStatus    int
		privateDetail string
	}{
		{
			name:   "invalid create configuration",
			method: http.MethodPost,
			path:   "/api/v1/repositories",
			body:   validBody,
			repositories: &recordingRepositoryService{
				createError: repository.ErrInvalidConfig,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "create name conflict",
			method: http.MethodPost,
			path:   "/api/v1/repositories",
			body:   validBody,
			repositories: &recordingRepositoryService{
				createError: repository.ErrNameConflict,
			},
			wantStatus: http.StatusConflict,
		},
		{
			name:   "remove missing repository",
			method: http.MethodDelete,
			path:   "/api/v1/repositories/" + repositoryID.String(),
			repositories: &recordingRepositoryService{
				removeError: repository.ErrNotFound,
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "list persistence failure",
			method: http.MethodGet,
			path:   "/api/v1/repositories",
			repositories: &recordingRepositoryService{
				listError: errors.New("database unavailable"),
			},
			wantStatus:    http.StatusInternalServerError,
			privateDetail: "database unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				context.Background(),
				test.method,
				test.path,
				bytes.NewBufferString(test.body),
			)
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()

			newAdminTestHandler(
				&recordingClusterService{},
				nil,
				test.repositories,
				nil,
				nil,
				nil,
				nil,
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if test.privateDetail != "" &&
				strings.Contains(response.Body.String(), test.privateDetail) {
				t.Error("internal error details were exposed in the problem response")
			}
		})
	}
}

func TestUpdateRepositoryCommitIndexing(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.New()
	repositories := &recordingRepositoryService{
		updated: repository.Repository{
			Config: repository.Config{
				Name:           "infrastructure",
				CloneURL:       "https://github.com/example/infrastructure.git",
				Branch:         "main",
				CommitIndexing: repository.CommitIndexingTrackedBranchHistory,
			},
			ID:        repositoryID,
			CreatedAt: time.Now().UTC(),
			Refresh: repository.RefreshState{
				Status: repository.RefreshStatusPending,
			},
		},
	}
	body := bytes.NewBufferString(`{"commitIndexing":"trackedBranchHistory"}`)
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPatch,
		"/api/v1/repositories/"+repositoryID.String(),
		body,
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newAdminTestHandler(&recordingClusterService{}, nil, repositories, nil, nil, nil, nil).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if repositories.updatedID != repositoryID ||
		repositories.updatedMode != repository.CommitIndexingTrackedBranchHistory {
		t.Errorf("update = (%s, %q)", repositories.updatedID, repositories.updatedMode)
	}

	var result api.Repository
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.CommitIndexing != api.RepositoryCommitIndexingTrackedBranchHistory {
		t.Errorf("commit indexing = %q", result.CommitIndexing)
	}
}

func TestRepositoryHandlersListAndRemove(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	failedRepositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	attemptedAt := time.Date(2026, time.October, 16, 12, 5, 0, 0, time.UTC)
	repositories := &recordingRepositoryService{
		items: []repository.Repository{
			{
				Name:      "infrastructure",
				CloneURL:  "https://github.com/example/infrastructure.git",
				Branch:    "main",
				ID:        repositoryID,
				CreatedAt: time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC),
				Refresh: repository.RefreshState{
					Status:      repository.RefreshStatusSucceeded,
					CommitSHA:   &commitSHA,
					AttemptedAt: &attemptedAt,
					SucceededAt: &attemptedAt,
				},
			},
			{
				Name:      "broken",
				CloneURL:  "https://github.com/example/broken.git",
				Branch:    "main",
				ID:        failedRepositoryID,
				CreatedAt: time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC),
				Refresh: repository.RefreshState{
					Status: repository.RefreshStatusFailed,
				},
			},
		},
	}
	httpHandler := newAdminTestHandler(&recordingClusterService{}, nil, repositories, nil, nil, nil, nil)

	listRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/repositories?refreshStatus=succeeded",
		nil,
	)
	listResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(listResponse, listRequest)

	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResponse.Code, http.StatusOK)
	}

	var list api.RepositoryList
	if err := json.NewDecoder(listResponse.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	if len(list.Items) != 1 || list.Items[0].ID != repositoryID ||
		list.Total != 1 ||
		list.Items[0].Refresh.Status != api.RepositoryRefreshStatusSucceeded ||
		list.Items[0].Refresh.Commit == nil ||
		list.Items[0].Refresh.Commit.Sha != commitSHA {
		t.Errorf("repository list = %#v", list.Items)
	}

	removeRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodDelete,
		"/api/v1/repositories/"+repositoryID.String(),
		nil,
	)
	removeResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(removeResponse, removeRequest)

	if removeResponse.Code != http.StatusNoContent {
		t.Fatalf("remove status = %d, want %d", removeResponse.Code, http.StatusNoContent)
	}

	if repositories.removedID != repositoryID {
		t.Errorf("removed ID = %s, want %s", repositories.removedID, repositoryID)
	}
}

type recordingRepositoryService struct {
	cfg         repository.Config
	created     repository.Repository
	updated     repository.Repository
	items       []repository.Repository
	updatedID   uuid.UUID
	updatedMode repository.CommitIndexingMode
	removedID   uuid.UUID
	createError error
	listError   error
	removeError error
}

func (r *recordingRepositoryService) CreateRepository(
	_ context.Context,
	cfg repository.Config,
) (repository.Repository, error) {
	r.cfg = cfg
	r.created.Config = cfg

	return r.created, r.createError
}

func (r *recordingRepositoryService) ListRepositories(context.Context) ([]repository.Repository, error) {
	return r.items, r.listError
}

func (r *recordingRepositoryService) UpdateRepositoryCommitIndexing(
	_ context.Context,
	repositoryID uuid.UUID,
	mode repository.CommitIndexingMode,
) (repository.Repository, error) {
	r.updatedID = repositoryID
	r.updatedMode = mode

	return r.updated, nil
}

func (r *recordingRepositoryService) RemoveRepository(_ context.Context, repositoryID uuid.UUID) error {
	r.removedID = repositoryID

	return r.removeError
}
