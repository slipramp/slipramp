package server

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
		"refreshCredentialName":"github-server",
		"checkoutCredentialName":"github-readonly"
	}`)
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/repositories",
		body,
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		repositories,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}

	wantLocation := "/api/v1/repositories/" + repositoryID.String()
	if location := response.Header().Get("Location"); location != wantLocation {
		t.Errorf("Location = %q, want %q", location, wantLocation)
	}

	if repositories.cfg.Name != "infrastructure" ||
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
		result.RefreshCredentialName == nil ||
		*result.RefreshCredentialName != "github-server" ||
		result.CreatedAt != createdAt ||
		result.Refresh.Status != api.RepositoryRefreshStatusPending {
		t.Errorf("repository response = %#v", result)
	}
}

func TestCreateRepositoryMapsExpectedFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{
			name:       "invalid configuration",
			err:        repository.ErrInvalidConfig,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "name conflict",
			err:        repository.ErrNameConflict,
			wantStatus: http.StatusConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repositories := &recordingRepositoryService{createError: test.err}
			body := bytes.NewBufferString(`{
				"name":"infrastructure",
				"cloneUrl":"https://github.com/example/infrastructure.git",
				"branch":"main"
			}`)
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/api/v1/repositories",
				body,
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			newHandler(
				&recordingClusterService{},
				repositories,
				nil,
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
		})
	}
}

func TestRepositoryHandlersListAndRemove(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
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
		},
	}
	httpHandler := newHandler(
		&recordingClusterService{},
		repositories,
		nil,
	)

	listRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/repositories",
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
		list.Items[0].Refresh.Status != api.RepositoryRefreshStatusSucceeded ||
		list.Items[0].Refresh.CommitSha == nil ||
		*list.Items[0].Refresh.CommitSha != commitSHA {
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
		t.Fatalf(
			"remove status = %d, want %d",
			removeResponse.Code,
			http.StatusNoContent,
		)
	}

	if repositories.removedID != repositoryID {
		t.Errorf("removed ID = %s, want %s", repositories.removedID, repositoryID)
	}
}

func TestRemoveRepositoryReturnsNotFound(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	repositories := &recordingRepositoryService{
		removeError: repository.ErrNotFound,
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodDelete,
		"/api/v1/repositories/"+repositoryID.String(),
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		repositories,
		nil,
	).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusNotFound)
}

func TestRepositoryHandlerDoesNotExposePersistenceFailures(t *testing.T) {
	t.Parallel()

	repositories := &recordingRepositoryService{
		listError: errors.New("database unavailable"),
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/repositories",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		repositories,
		nil,
	).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusInternalServerError)
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Error("internal error details were exposed in the problem response")
	}
}

type recordingRepositoryService struct {
	cfg         repository.Config
	created     repository.Repository
	items       []repository.Repository
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

func (r *recordingRepositoryService) ListRepositories(
	context.Context,
) ([]repository.Repository, error) {
	return r.items, r.listError
}

func (r *recordingRepositoryService) RemoveRepository(
	_ context.Context,
	repositoryID uuid.UUID,
) error {
	r.removedID = repositoryID

	return r.removeError
}
