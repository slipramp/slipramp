package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestRunReviewApproveAndReject(t *testing.T) {
	t.Parallel()

	requests := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/runs/"+testRunID+"/review" {
			http.NotFound(writer, request)

			return
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests = append(requests, body)
		response := runResponse("queued", nil)
		response["review"] = map[string]any{"state": body["decision"], "comment": body["comment"]}
		writeJSON(t, writer, http.StatusOK, response)
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "review", "approve", testRunID, "--comment", "  reviewed plan  ",
	)
	if code != cli.ExitValid || stderr != "" || !strings.Contains(output, "Review: approved") ||
		!strings.Contains(output, "Review comment: reviewed plan") {
		t.Fatalf("approve exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}

	code, output, stderr = runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "review", "reject", testRunID, "--format", "json",
	)
	if code != cli.ExitValid || stderr != "" || !strings.Contains(output, `"state": "rejected"`) {
		t.Fatalf("reject exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
	if len(requests) != 2 || requests[0]["decision"] != "approved" || requests[0]["comment"] != "reviewed plan" ||
		requests[1]["decision"] != "rejected" {
		t.Errorf("requests = %#v", requests)
	}
}

func TestRunReviewRejectsOversizedComment(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	comment := strings.Repeat("a", 4097)
	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "review", "approve", testRunID, "--comment", comment,
	)
	if code != cli.ExitInvocation || output != "" || !strings.Contains(stderr, "at most 4096") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}
