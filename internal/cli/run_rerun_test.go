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

func TestRunRerunSubmitsModeAndIdempotencyKey(t *testing.T) {
	t.Parallel()

	const idempotencyKey = "rerun-request-1"
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/runs/"+testRunID+"/rerun" {
			http.NotFound(writer, request)

			return
		}
		if got := request.Header.Get("Idempotency-Key"); got != idempotencyKey {
			t.Errorf("Idempotency-Key = %q", got)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		response := runResponse("queued", nil)
		response["rerunOfRunId"] = testRunID
		response["rerunMode"] = "exact"
		writeJSON(t, writer, http.StatusCreated, response)
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "rerun", testRunID, "--mode", "exact", "--idempotency-key", idempotencyKey,
		"--format", "json",
	)
	if code != cli.ExitValid || stderr != "" || !strings.Contains(output, `"rerunMode": "exact"`) {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
	if requestBody["mode"] != "exact" {
		t.Errorf("request body = %#v", requestBody)
	}

	code, output, stderr = runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "rerun", testRunID, "--mode", "current",
	)
	if code != cli.ExitInvocation || output != "" || !strings.Contains(stderr, "exact") {
		t.Fatalf("invalid mode exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}
