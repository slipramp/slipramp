package cli_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestRunCancelPrintsCanonicalState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/runs/"+testRunID+"/cancel" {
			http.NotFound(writer, request)

			return
		}
		response := runResponse("running", nil)
		response["cancellationRequestedAt"] = "2026-10-25T12:00:30Z"
		writeJSON(t, writer, http.StatusOK, response)
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(t, context.Background(), server, "run", "cancel", testRunID)
	if code != cli.ExitValid || stderr != "" ||
		!strings.Contains(output, "Phase: running") || !strings.Contains(output, "Cancellation: requested at") {
		t.Fatalf("text exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}

	code, output, stderr = runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "cancel", testRunID, "--format", "json",
	)
	if code != cli.ExitValid || stderr != "" || !strings.Contains(output, `"cancellationRequestedAt"`) {
		t.Fatalf("JSON exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}

func TestRunGetCancelAndProblemRendering(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/runs/"+testRunID:
			outcome := "failed"
			writeJSON(t, writer, http.StatusOK, runResponse("finished", &outcome))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/runs/"+testRunID+"/cancel":
			writeProblem(writer, http.StatusConflict, "The Run already finished.")
		default:
			writeProblem(writer, http.StatusUnauthorized, "Use a valid API key.")
		}
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "get", testRunID, "--format", "json",
	)
	if code != cli.ExitValid || stderr != "" || !strings.Contains(output, `"outcome": "failed"`) {
		t.Fatalf("get exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}

	code, _, stderr = runRemoteApp(t, context.Background(), server, "run", "cancel", testRunID)
	if code != cli.ExitRemote || !strings.Contains(stderr, "conflict (HTTP 409): The Run already finished.") {
		t.Fatalf("cancel exit = %d, stderr = %q", code, stderr)
	}

	code, _, stderr = runRemoteApp(t, context.Background(), server, "target", "list")
	if code != cli.ExitRemote || !strings.Contains(stderr, "authentication failed (HTTP 401): Use a valid API key.") {
		t.Fatalf("list exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stderr, "test-api-key") {
		t.Errorf("stderr leaks credential: %q", stderr)
	}
}
