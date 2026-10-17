package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/healthz",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(&recordingClusterService{}, nil, nil, nil, nil).ServeHTTP(
		response,
		request,
	)
	expectedBody := "{\"status\":\"ok\"}\n"

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}

	if response.Body.String() != expectedBody {
		t.Errorf(
			"body = %q, want %q",
			response.Body.String(),
			expectedBody,
		)
	}

	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf(
			"Content-Type = %q, want %q",
			contentType,
			"application/json",
		)
	}
}

func TestHealthEndpointRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/healthz",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(&recordingClusterService{}, nil, nil, nil, nil).ServeHTTP(
		response,
		request,
	)

	assertProblem(t, response, http.StatusMethodNotAllowed)
}
