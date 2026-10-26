package remote_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/cli/remote"
)

func TestNewValidatesConnectionConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		server string
		key    string
		want   string
	}{
		{name: "missing URL", key: "key", want: "server URL is required"},
		{name: "relative URL", server: "server.local", key: "key", want: "absolute http:// or https://"},
		{name: "URL credentials", server: "https://user:pass@server.local", key: "key", want: "without credentials"},
		{name: "unsupported scheme", server: "ftp://server.local", key: "key", want: "absolute http:// or https://"},
		{name: "missing API key", server: "https://server.local", want: "SLIPRAMP_API_KEY"},
		{name: "invalid API key", server: "https://server.local", key: " key", want: "invalid whitespace"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := remote.New(test.server, test.key, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestClientInjectsBearerAndScrubsCredential(t *testing.T) {
	t.Parallel()

	const key = "api-key-must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer "+key {
			t.Errorf("Authorization = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[],"total":0,"limit":25,"offset":0}`))
	}))
	defer server.Close()

	client, err := remote.New(server.URL, key, server.Client())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response, err := client.API.ListTargets(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTargets() error = %v", err)
	}
	_ = response.Body.Close()

	for _, rendered := range []string{
		client.NetworkError("request", errors.New("failed with "+key)).Error(),
		client.ResponseError("request", http.StatusBadRequest, []byte(
			`{"type":"about:blank","title":"bad","status":400,"detail":"rejected `+key+`"}`,
		)).Error(),
	} {
		if strings.Contains(rendered, key) || !strings.Contains(rendered, "[REDACTED]") {
			t.Errorf("rendered error = %q", rendered)
		}
	}
}
