package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestRunCreateValidatesAndSubmitsParameters(t *testing.T) {
	t.Parallel()

	const explicitKey = "automation-request-7"
	var createdBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/targets/"+testTargetID:
			writeJSON(t, writer, http.StatusOK, runTargetResponse())
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/runs":
			if got := request.Header.Get("Idempotency-Key"); got != explicitKey {
				t.Errorf("Idempotency-Key = %q", got)
			}
			if err := json.NewDecoder(request.Body).Decode(&createdBody); err != nil {
				t.Errorf("decode request: %v", err)
			}
			writeJSON(t, writer, http.StatusCreated, runResponse("queued", nil))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "create", "--target", testTargetID, "--action", "preview",
		"--set", "force=true", "--set", "count=2.5", "--set", "mode=safe",
		"--idempotency-key", explicitKey,
	)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{testRunID, "queued", "storage", "preview", "/runs/" + testRunID} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %q", want, output)
		}
	}
	parameters := createdBody["parameters"].(map[string]any)
	if parameters["force"] != true || parameters["count"] != 2.5 || parameters["mode"] != "safe" {
		t.Errorf("parameters = %#v", parameters)
	}

	code, _, stderr = runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "create", "--target", testTargetID, "--action", "preview", "--set", "unknown=value",
	)
	if code != cli.ExitInvalid || !strings.Contains(stderr, "not declared") {
		t.Fatalf("invalid parameter exit = %d, stderr = %q", code, stderr)
	}

	code, _, stderr = runRemoteApp(
		t,
		context.Background(),
		server,
		"run", "create", "--target", testTargetID, "--action", "preview", "--set", "force=true",
	)
	if code != cli.ExitInvalid || !strings.Contains(stderr, "required parameter \"mode\"") {
		t.Fatalf("missing parameter exit = %d, stderr = %q", code, stderr)
	}
}

func TestRunCreateGeneratesFreshIdempotencyKeys(t *testing.T) {
	t.Parallel()

	var keysMu sync.Mutex
	keys := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			writeJSON(t, writer, http.StatusOK, runTargetResponse())
		case http.MethodPost:
			keysMu.Lock()
			keys = append(keys, request.Header.Get("Idempotency-Key"))
			keysMu.Unlock()
			writeJSON(t, writer, http.StatusCreated, runResponse("queued", nil))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	for range 2 {
		code, _, stderr := runRemoteApp(
			t,
			context.Background(),
			server,
			"run", "create", "--target", testTargetID, "--action", "preview", "--set", "mode=safe",
		)
		if code != cli.ExitValid || stderr != "" {
			t.Fatalf("exit = %d, stderr = %q", code, stderr)
		}
	}

	keysMu.Lock()
	defer keysMu.Unlock()
	if len(keys) != 2 || keys[0] == "" || keys[1] == "" || keys[0] == keys[1] {
		t.Errorf("idempotency keys = %#v", keys)
	}
}
