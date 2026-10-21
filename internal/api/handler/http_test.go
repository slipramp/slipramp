package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/api"
)

var pathParameterPattern = regexp.MustCompile(`\{[^/]+\}`)

func TestHTTPRejectsUnsupportedMethodsForEveryOperationPath(t *testing.T) {
	t.Parallel()

	router := newHTTP(New(Dependencies{}), HTTPOptions{})
	if len(router.allowedMethods) == 0 {
		t.Fatal("generated router registered no operation paths")
	}

	for path, registeredMethods := range router.allowedMethods {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			allowedMethods := slices.Clone(registeredMethods)
			slices.Sort(allowedMethods)
			request := httptest.NewRequestWithContext(
				context.Background(),
				unsupportedMethod(allowedMethods),
				pathParameterPattern.ReplaceAllString(path, "test"),
				nil,
			)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
			if allowed := response.Header().Get("Allow"); allowed != strings.Join(allowedMethods, ", ") {
				t.Errorf("Allow = %q, want %q", allowed, strings.Join(allowedMethods, ", "))
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", contentType)
			}

			var problem api.Problem
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem response: %v", err)
			}
			if problem.Status != http.StatusMethodNotAllowed {
				t.Errorf("problem status = %d, want %d", problem.Status, http.StatusMethodNotAllowed)
			}
		})
	}
}

func unsupportedMethod(allowedMethods []string) string {
	for _, method := range []string{
		http.MethodPatch,
		http.MethodDelete,
		http.MethodPost,
		http.MethodPut,
		http.MethodGet,
	} {
		if !slices.Contains(allowedMethods, method) {
			return method
		}
	}

	return http.MethodOptions
}
