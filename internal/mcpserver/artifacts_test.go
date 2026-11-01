package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

func TestHTTPReadRunLogWithoutArtifact(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1260")

	t.Run("finished", func(t *testing.T) {
		t.Parallel()

		runs := &recordingRuns{record: run.Run{ID: runID, Phase: run.PhaseFinished}}
		handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, runs, nil)
		session := connectTestClient(t, handler, testAPIKey)

		result := callTool(t, session, "read_run_log", map[string]any{"runId": runID.String()})
		var page readRunLogOutput
		decodeStructuredContent(t, result, &page)

		if page.RunID != runID.String() || page.Available || page.Encoding != "utf-8" ||
			page.Contents != "" || page.OffsetBytes != 0 || page.NextOffsetBytes != 0 ||
			page.DurableSizeBytes != 0 || page.HasMore || !page.Complete || page.Failed {
			t.Errorf("read_run_log output = %#v", page)
		}
		if page.ArtifactID != "" || page.State != "" || page.RetentionState != "" {
			t.Errorf("read_run_log Artifact fields = %#v", page)
		}
		if runs.getRunID != runID {
			t.Errorf("GetRun ID = %s, want %s", runs.getRunID, runID)
		}

		var structured map[string]any
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("marshal structured content: %v", err)
		}
		if err := json.Unmarshal(encoded, &structured); err != nil {
			t.Fatalf("unmarshal structured content: %v", err)
		}
		for _, field := range []string{"artifactId", "state", "retentionState"} {
			if _, found := structured[field]; found {
				t.Errorf("read_run_log output contains %q: %#v", field, structured)
			}
		}
	})

	t.Run("unfinished", func(t *testing.T) {
		t.Parallel()

		runs := &recordingRuns{record: run.Run{ID: runID, Phase: run.PhasePreparing}}
		handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, runs, nil)
		session := connectTestClient(t, handler, testAPIKey)

		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name:      "read_run_log",
			Arguments: map[string]any{"runId": runID.String()},
		})
		if err != nil {
			t.Fatalf("CallTool() error = %v", err)
		}
		if !result.IsError {
			t.Fatalf("CallTool() result = %#v, want tool error", result)
		}
		encoded, _ := json.Marshal(result.Content)
		if !strings.Contains(string(encoded), "the Run log does not exist yet") {
			t.Errorf("CallTool() content = %s", encoded)
		}
	})

	t.Run("finished offset past empty log", func(t *testing.T) {
		t.Parallel()

		runs := &recordingRuns{record: run.Run{ID: runID, Phase: run.PhaseFinished}}
		handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, runs, nil)
		session := connectTestClient(t, handler, testAPIKey)

		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "read_run_log",
			Arguments: map[string]any{
				"runId":       runID.String(),
				"offsetBytes": 1,
			},
		})
		if err != nil {
			t.Fatalf("CallTool() error = %v", err)
		}
		if !result.IsError {
			t.Fatalf("CallTool() result = %#v, want tool error", result)
		}
		encoded, _ := json.Marshal(result.Content)
		if !strings.Contains(string(encoded), "offsetBytes exceeds the durable Run log size") {
			t.Errorf("CallTool() content = %s", encoded)
		}
	})
}
