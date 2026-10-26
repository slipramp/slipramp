package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

const (
	testTargetID   = "018f90ff-9fd8-7b5f-b7e1-f32ff54c1202"
	testRunID      = "018f90ff-9fd8-7b5f-b7e1-f32ff54c1203"
	testArtifactID = "018f90ff-9fd8-7b5f-b7e1-f32ff54c1205"
)

func runRemoteApp(
	t *testing.T,
	ctx context.Context,
	server *httptest.Server,
	arguments ...string,
) (int, string, string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.App{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Context:    ctx,
		HTTPClient: server.Client(),
		Getenv: func(name string) string {
			switch name {
			case "SLIPRAMP_URL":
				return server.URL
			case "SLIPRAMP_API_KEY":
				return "test-api-key"
			default:
				return ""
			}
		},
	}.Run(arguments)

	return code, stdout.String(), stderr.String()
}

func runTargetResponse() map[string]any {
	return map[string]any{
		"id": testTargetID, "repositoryId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
		"repositoryName": "infrastructure", "catalogRevisionId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1204",
		"catalogCommit": map[string]any{"sha": strings.Repeat("a", 40)},
		"definition": map[string]any{
			"apiVersion": "slipramp.com/v1alpha1", "kind": "Target",
			"metadata": map[string]any{"name": "storage"}, "source": map[string]any{"path": ".slipramp/storage.yaml", "document": 1},
			"status": map[string]any{
				"desiredCommit":  map[string]any{"sha": strings.Repeat("b", 40)},
				"reconciliation": map[string]any{"comparison": "unknown"},
			},
			"spec": map[string]any{"actions": map[string]any{
				"preview": map[string]any{
					"command": []string{"terraform", "plan"},
					"parameters": map[string]any{
						"force": map[string]any{"type": "boolean", "argument": map[string]any{"name": "--force"}},
						"count": map[string]any{"type": "number", "argument": map[string]any{"name": "--count"}},
						"mode": map[string]any{
							"type": "enum", "required": true, "values": []string{"safe", "fast"},
							"argument": map[string]any{"name": "--mode"},
						},
					},
				},
			}},
		},
	}
}

func runResponse(phase string, outcome *string) map[string]any {
	response := map[string]any{
		"id": testRunID, "phase": phase, "targetId": testTargetID,
		"repositoryId":      "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
		"catalogRevisionId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1204",
		"commit":            map[string]any{"sha": strings.Repeat("a", 40)},
		"target":            "storage", "action": "preview", "parameters": map[string]any{},
		"runtime": "terraform", "placement": "admin", "cluster": "colo",
		"source": map[string]any{"type": "api"}, "inputs": map[string]any{},
		"artifactConsumers": map[string]any{}, "review": map[string]any{"state": "not-required"},
		"createdAt": "2026-10-25T12:00:00Z",
	}
	if outcome != nil {
		response["outcome"] = *outcome
		response["finishedAt"] = "2026-10-25T12:01:00Z"
		if *outcome != "succeeded" {
			response["outcomeReason"] = "ProcessExited"
			response["outcomeMessage"] = "The process exited unsuccessfully."
		}
	}

	return response
}

func writeJSON(t *testing.T, writer http.ResponseWriter, status int, value any) {
	t.Helper()

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func writeProblem(writer http.ResponseWriter, status int, detail string) {
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)
	_, _ = fmt.Fprintf(
		writer,
		`{"type":"about:blank","title":%q,"status":%d,"detail":%q}`,
		http.StatusText(status),
		status,
		detail,
	)
}
