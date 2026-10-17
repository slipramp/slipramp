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
	"github.com/slipramp/slipramp/internal/run"
)

func TestCreateRunReturnsQueuedRun(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	createdAt := time.Date(2026, time.October, 16, 12, 5, 0, 0, time.UTC)
	runs := &recordingRunService{
		created: run.Run{
			ID:                runID,
			State:             run.StateQueued,
			RepositoryID:      repositoryID,
			CatalogRevisionID: revisionID,
			CommitSHA:         "0123456789abcdef0123456789abcdef01234567",
			TargetName:        "network",
			ActionName:        "preview",
			Parameters:        map[string]any{"mode": "safe", "refresh": true},
			PlacementName:     "infrastructure",
			ClusterName:       "colo",
			Source:            run.Source{Type: run.SourceTypeManual},
			CreatedAt:         createdAt,
		},
	}
	body := bytes.NewBufferString(`{
		"repositoryId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
		"target":"network",
		"action":"preview",
		"parameters":{"mode":"safe","refresh":true},
		"source":{"type":"manual"}
	}`)
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/runs",
		body,
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "manual-network-preview")
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		nil,
		nil,
		runs,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body)
	}
	if runs.request.RepositoryID != repositoryID ||
		runs.request.TargetName != "network" ||
		runs.request.ActionName != "preview" ||
		runs.request.Source.Type != run.SourceTypeManual ||
		runs.request.IdempotencyKey == nil ||
		*runs.request.IdempotencyKey != "manual-network-preview" ||
		runs.request.Parameters["mode"] != "safe" ||
		runs.request.Parameters["refresh"] != true {
		t.Errorf("creation request = %#v", runs.request)
	}

	var result api.Run
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.ID != runID || result.State != api.Queued ||
		result.CatalogRevisionID != revisionID || result.Cluster != "colo" ||
		!result.CreatedAt.Equal(createdAt) {
		t.Errorf("Run response = %#v", result)
	}
	mode, err := result.Parameters["mode"].AsParameterValue0()
	if err != nil || mode != "safe" {
		t.Errorf("mode parameter = %q, error = %v", mode, err)
	}
}

func TestCreateRunMapsExpectedFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		wantStatus    int
		privateDetail string
	}{
		{
			name:       "invalid parameters",
			err:        run.ErrInvalidParameters,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing action",
			err:        run.ErrActionNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "idempotency conflict",
			err:        run.ErrIdempotencyConflict,
			wantStatus: http.StatusConflict,
		},
		{
			name:          "persistence failure",
			err:           errors.New("database unavailable"),
			wantStatus:    http.StatusInternalServerError,
			privateDetail: "database unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := bytes.NewBufferString(`{
				"repositoryId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
				"target":"network",
				"action":"preview",
				"source":{"type":"api"}
			}`)
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/api/v1/runs",
				body,
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			newHandler(
				&recordingClusterService{},
				nil,
				nil,
				&recordingRunService{err: test.err},
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

type recordingRunService struct {
	request run.CreateRequest
	created run.Run
	err     error
}

func (s *recordingRunService) CreateRun(
	_ context.Context,
	request run.CreateRequest,
) (run.Run, error) {
	s.request = request

	return s.created, s.err
}
