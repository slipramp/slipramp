package cli_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestRunWaitPollsUntilSucceeded(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/runs/"+testRunID {
			http.NotFound(writer, request)

			return
		}
		if requests.Add(1) == 1 {
			writeJSON(t, writer, http.StatusOK, runResponse("running", nil))

			return
		}
		outcome := "succeeded"
		writeJSON(t, writer, http.StatusOK, runResponse("finished", &outcome))
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "wait", testRunID, "--poll-interval", "1ms",
	)
	if code != cli.ExitValid || stderr != "" || requests.Load() != 2 ||
		!strings.Contains(output, "Phase: finished") || !strings.Contains(output, "Outcome: succeeded") {
		t.Fatalf("exit = %d, requests = %d, stdout = %q, stderr = %q", code, requests.Load(), output, stderr)
	}
}

func TestRunWaitReturnsFinishedFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		outcome := "failed"
		writeJSON(t, writer, http.StatusOK, runResponse("finished", &outcome))
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "wait", testRunID, "--format", "json",
	)
	if code != cli.ExitRunUnsuccessful || stderr != "" || !strings.Contains(output, `"outcome": "failed"`) {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}

func TestRunWaitTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(t, writer, http.StatusOK, runResponse("running", nil))
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "wait", testRunID, "--timeout", "5ms", "--poll-interval", "100ms",
	)
	if code != cli.ExitRemote || output != "" || !strings.Contains(stderr, "context deadline exceeded") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}

func TestRunWaitRejectsFinishedRunWithoutOutcome(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(t, writer, http.StatusOK, runResponse("finished", nil))
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(t, context.Background(), server, "run", "wait", testRunID)
	if code != cli.ExitRemote || output != "" || !strings.Contains(stderr, "finished Run has no valid outcome") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}
