package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrivateAPICacheControl(t *testing.T) {
	t.Parallel()

	httpHandler := NewHTTP(Dependencies{}, HTTPOptions{})
	tests := []struct {
		path string
		want string
	}{
		{path: "/api/v1/unknown", want: privateNoStoreCacheControl},
		{path: "/controller/v1/unknown", want: privateNoStoreCacheControl},
		{path: "/execution/v1/unknown", want: privateNoStoreCacheControl},
		{path: "/healthz"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			httpHandler.ServeHTTP(response, request)

			if got := response.Header().Get("Cache-Control"); got != test.want {
				t.Errorf("Cache-Control = %q, want %q", got, test.want)
			}
		})
	}
}
