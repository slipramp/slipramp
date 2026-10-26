package cli_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestTargetListFiltersTextAndJSON(t *testing.T) {
	t.Parallel()

	var requestsMu sync.Mutex
	requests := []url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization = %q", got)
		}
		requestsMu.Lock()
		requests = append(requests, request.URL.Query())
		requestsMu.Unlock()
		writeJSON(t, writer, http.StatusOK, map[string]any{
			"items": []map[string]any{{
				"id": testTargetID, "repositoryId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
				"repositoryName": "infrastructure", "catalogRevisionId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1204",
				"catalogCommit": map[string]any{"sha": strings.Repeat("a", 40)},
				"name":          "storage", "title": "Storage", "actions": []string{"preview", "sync"},
				"runtime": "terraform", "placement": "admin",
				"desiredCommit":  map[string]any{"sha": strings.Repeat("b", 40)},
				"reconciliation": map[string]any{"comparison": "different"},
			}},
			"total": 1, "limit": 10, "offset": 5,
		})
	}))
	defer server.Close()

	arguments := []string{
		"target", "list", "--query", "store", "--repository-id", "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
		"--runtime", "terraform", "--placement", "admin", "--limit", "10", "--offset", "5",
	}
	code, output, stderr := runRemoteApp(t, context.Background(), server, arguments...)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("text exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{testTargetID, "storage", "Storage", "infrastructure", "different", "preview,sync"} {
		if !strings.Contains(output, want) {
			t.Errorf("text output missing %q: %q", want, output)
		}
	}

	arguments = append(arguments, "--format", "json")
	code, output, stderr = runRemoteApp(t, context.Background(), server, arguments...)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("JSON exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(output, `"repositoryName": "infrastructure"`) ||
		!strings.Contains(output, `"total": 1`) {
		t.Errorf("JSON output = %q", output)
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	for _, query := range requests {
		for name, want := range map[string]string{
			"q": "store", "repositoryId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
			"runtime": "terraform", "placement": "admin", "limit": "10", "offset": "5",
		} {
			if got := query.Get(name); got != want {
				t.Errorf("query %s = %q, want %q", name, got, want)
			}
		}
	}
}
