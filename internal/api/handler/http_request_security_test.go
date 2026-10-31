package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestProtectedRequestsAuthenticateBeforeReadingBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		token   string
		handler http.Handler
	}{
		{
			name:  "product API",
			path:  "/api/v1/repositories",
			token: "invalid-product-token",
			handler: NewHTTP(
				Dependencies{ProductAuthentication: &recordingProductAuthenticationService{
					authenticationErr: productidentity.ErrUnauthenticated,
				}},
				HTTPOptions{},
			),
		},
		{
			name:  "controller enrollment",
			path:  "/controller/v1/enroll",
			token: "invalid-enrollment-token",
			handler: NewHTTP(
				Dependencies{},
				HTTPOptions{ControllerEnrollmentTokens: map[string]string{"colo": "valid-enrollment-token"}},
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := &countingRequestBody{}
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, test.path, body)
			request.Header.Set("Authorization", "Bearer "+test.token)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			test.handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if body.reads != 0 {
				t.Errorf("body reads = %d, want 0", body.reads)
			}
		})
	}
}

func TestRequestBodyLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		pattern       string
		contentLength int64
		wantStatus    int
	}{
		{
			name:          "public enrollment limit",
			path:          "/controller/v1/enroll",
			pattern:       "POST /controller/v1/enroll",
			contentLength: publicRequestBodyLimit + 1,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			name:          "authenticated request above public limit",
			path:          "/api/v1/repositories",
			pattern:       "POST /api/v1/repositories",
			contentLength: publicRequestBodyLimit + 1,
			wantStatus:    http.StatusNoContent,
		},
		{
			name:          "authenticated request safety limit",
			path:          "/api/v1/repositories",
			pattern:       "POST /api/v1/repositories",
			contentLength: authenticatedRequestBodyLimit + 1,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			name:          "authenticated artifact stream",
			path:          "/execution/v1/runs/018f90ff-9fd8-7b5f-b7e1-f32ff54c1700/artifacts/plan",
			pattern:       uploadRunArtifactPattern,
			contentLength: authenticatedRequestBodyLimit + 1,
			wantStatus:    http.StatusNoContent,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				test.path,
				io.NopCloser(strings.NewReader("body")),
			)
			request.Pattern = test.pattern
			request.ContentLength = test.contentLength
			response := httptest.NewRecorder()
			handler := limitRequestBodies(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			}))

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

type countingRequestBody struct {
	reads int
}

func (b *countingRequestBody) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("body must not be read")
}

func (*countingRequestBody) Close() error {
	return nil
}
