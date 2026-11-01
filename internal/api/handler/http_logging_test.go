package handler

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPLogsUseResolvedClientAddressWithoutQuery(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := logHTTPRequests(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}), logger, 1)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/callback?code=secret", nil)
	request.RemoteAddr = "203.0.113.10:443"
	request.Header.Set("X-Forwarded-For", "192.0.2.10")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	logged := output.String()
	if !strings.Contains(logged, `"client_ip":"192.0.2.10"`) ||
		!strings.Contains(logged, `"status":204`) {
		t.Fatalf("HTTP log = %s", logged)
	}
	if strings.Contains(logged, "code=secret") {
		t.Fatalf("HTTP log contains callback query: %s", logged)
	}
}
