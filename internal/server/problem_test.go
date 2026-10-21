package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slipramp/slipramp/internal/api"
)

func TestUnknownRouteReturnsProblem(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/unknown", nil)
	response := httptest.NewRecorder()

	newAdminTestHandler(&recordingClusterService{}, nil, nil, nil, nil, nil, nil).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusNotFound)
}

func assertProblem(t *testing.T, response *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()

	if response.Code != wantStatus {
		t.Errorf("status = %d, want %d", response.Code, wantStatus)
	}

	if contentType := response.Header().Get("Content-Type"); contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}

	var problem api.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem response: %v", err)
	}

	if problem.Type != "about:blank" {
		t.Errorf("problem type = %q, want %q", problem.Type, "about:blank")
	}

	if problem.Title != http.StatusText(wantStatus) {
		t.Errorf("problem title = %q, want %q", problem.Title, http.StatusText(wantStatus))
	}

	if problem.Status != wantStatus {
		t.Errorf("problem status = %d, want %d", problem.Status, wantStatus)
	}
}
