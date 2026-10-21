package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/readiness"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	newAdminTestHandler(&recordingClusterService{}, nil, nil, nil, nil, nil, nil).ServeHTTP(response, request)
	expectedBody := "{\"status\":\"ok\"}\n"

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}

	if response.Body.String() != expectedBody {
		t.Errorf("body = %q, want %q", response.Body.String(), expectedBody)
	}

	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}
}

func TestHealthEndpointRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/healthz", nil)
	response := httptest.NewRecorder()

	newAdminTestHandler(&recordingClusterService{}, nil, nil, nil, nil, nil, nil).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusMethodNotAllowed)
}

func TestHealthEndpointDoesNotCheckDependencies(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	httpHandler := newSystemTestHandler(readiness.Report{
		Database:        readiness.DependencyUnavailable,
		ArtifactStorage: readiness.DependencyUnavailable,
	})

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestReadinessEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		report     readiness.Report
		wantStatus int
		wantReady  api.ReadinessResponseStatus
	}{
		{
			name: "ready",
			report: readiness.Report{
				Database:        readiness.DependencyAvailable,
				ArtifactStorage: readiness.DependencyAvailable,
			},
			wantStatus: http.StatusOK,
			wantReady:  api.Ready,
		},
		{
			name: "database unavailable",
			report: readiness.Report{
				Database:        readiness.DependencyUnavailable,
				ArtifactStorage: readiness.DependencyAvailable,
			},
			wantStatus: http.StatusServiceUnavailable,
			wantReady:  api.NotReady,
		},
		{
			name: "artifact storage unavailable",
			report: readiness.Report{
				Database:        readiness.DependencyAvailable,
				ArtifactStorage: readiness.DependencyUnavailable,
			},
			wantStatus: http.StatusServiceUnavailable,
			wantReady:  api.NotReady,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()
			httpHandler := newSystemTestHandler(test.report)

			httpHandler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", contentType)
			}

			var body api.ReadinessResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode readiness response: %v", err)
			}
			if body.Status != test.wantReady {
				t.Errorf("response status = %q, want %q", body.Status, test.wantReady)
			}
			if body.Database != api.DependencyStatus(test.report.Database) {
				t.Errorf("database = %q, want %q", body.Database, test.report.Database)
			}
			if body.ArtifactStorage != api.DependencyStatus(test.report.ArtifactStorage) {
				t.Errorf("artifact storage = %q, want %q", body.ArtifactStorage, test.report.ArtifactStorage)
			}
		})
	}
}

func TestReadinessEndpointRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/readyz", nil)
	response := httptest.NewRecorder()

	newAdminTestHandler(&recordingClusterService{}, nil, nil, nil, nil, nil, nil).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusMethodNotAllowed)
}

func newSystemTestHandler(report readiness.Report) http.Handler {
	return newHTTPHandler(
		&recordingClusterService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		staticReadinessService{report: report},
		nil,
		nil,
		nil,
	)
}
