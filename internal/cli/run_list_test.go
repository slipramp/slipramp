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

func TestRunListFiltersTextAndJSON(t *testing.T) {
	t.Parallel()

	var requestsMu sync.Mutex
	requests := []url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/runs" {
			http.NotFound(writer, request)

			return
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization = %q", got)
		}
		requestsMu.Lock()
		requests = append(requests, request.URL.Query())
		requestsMu.Unlock()
		runRecord := runResponse("running", nil)
		runRecord["observationState"] = "current"
		runRecord["review"] = map[string]any{"state": "approved"}
		writeJSON(t, writer, http.StatusOK, map[string]any{
			"items": []map[string]any{runRecord},
			"total": 1, "limit": 10, "offset": 5,
		})
	}))
	defer server.Close()

	commitSHA := strings.Repeat("a", 40)
	arguments := []string{
		"run", "list",
		"--repository-id", "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
		"--target-id", testTargetID,
		"--commit-sha", commitSHA,
		"--input-artifact-id", testArtifactID,
		"--cluster", "colo",
		"--phase", "running",
		"--review-state", "approved",
		"--limit", "10",
		"--offset", "5",
	}
	code, output, stderr := runRemoteApp(t, context.Background(), server, arguments...)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("text exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		testRunID, "storage", "preview", "running", "approved", "current", "colo", "api",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("text output missing %q: %q", want, output)
		}
	}

	arguments = append(arguments, "--format", "json")
	code, output, stderr = runRemoteApp(t, context.Background(), server, arguments...)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("JSON exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(output, `"phase": "running"`) || !strings.Contains(output, `"total": 1`) {
		t.Errorf("JSON output = %q", output)
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	for _, query := range requests {
		for name, want := range map[string]string{
			"repositoryId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
			"targetId":     testTargetID, "commitSha": commitSHA, "inputArtifactId": testArtifactID,
			"cluster": "colo", "phase": "running", "reviewState": "approved", "limit": "10", "offset": "5",
		} {
			if got := query.Get(name); got != want {
				t.Errorf("query %s = %q, want %q", name, got, want)
			}
		}
	}
}

func TestRunListRejectsInvalidFilters(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	tests := []struct {
		name      string
		arguments []string
		message   string
	}{
		{
			name:      "commit repository",
			arguments: []string{"--commit-sha", strings.Repeat("a", 40)},
			message:   "requires --repository-id",
		},
		{
			name:      "commit shape",
			arguments: []string{"--repository-id", testTargetID, "--commit-sha", "abc"},
			message:   "full lowercase",
		},
		{name: "phase", arguments: []string{"--phase", "done"}, message: "unsupported --phase"},
		{name: "review", arguments: []string{"--review-state", "waiting"}, message: "unsupported --review-state"},
		{name: "artifact", arguments: []string{"--input-artifact-id", "not-a-uuid"}, message: "must be a UUID"},
		{name: "limit", arguments: []string{"--limit", "101"}, message: "between 1 and 100"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			arguments := append([]string{"run", "list"}, test.arguments...)
			code, output, stderr := runRemoteApp(t, context.Background(), server, arguments...)
			if code != cli.ExitInvocation || output != "" || !strings.Contains(stderr, test.message) {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
			}
		})
	}
}
