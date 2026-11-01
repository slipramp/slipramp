package mcpserver

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

func TestHTTPListRunsAcceptsHistoryFilters(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1250")
	targetID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1251")
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1252")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1253")
	commitSHA := testCommitSHA("d")
	record := run.Run{
		ID:                runID,
		Phase:             run.PhaseFinished,
		TargetID:          targetID,
		RepositoryID:      repositoryID,
		CatalogRevisionID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1254"),
		CommitSHA:         commitSHA,
		TargetName:        "dns",
		ActionName:        "apply",
		Parameters:        map[string]any{},
		PlacementName:     "homelab",
		ClusterName:       "homelab",
		Source:            run.Source{Type: run.SourceTypeMCP},
		Review:            run.Review{State: run.ReviewStateApproved},
		CreatedAt:         time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC),
	}
	runs := &recordingRuns{listed: run.RunList{Items: []run.Run{record}, Total: 7}}
	handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, runs, nil)
	session := connectTestClient(t, handler, testAPIKey)

	result := callTool(t, session, "list_runs", map[string]any{
		"repositoryId":    repositoryID.String(),
		"targetId":        targetID.String(),
		"commitSha":       commitSHA,
		"inputArtifactId": artifactID.String(),
		"cluster":         "homelab",
		"phase":           "finished",
		"reviewState":     "approved",
		"limit":           2,
		"offset":          1,
	})
	var listed runListOutput
	decodeStructuredContent(t, result, &listed)

	if listed.Total != 7 || listed.Limit != 2 || listed.Offset != 1 ||
		len(listed.Items) != 1 || listed.Items[0].ID != runID.String() {
		t.Errorf("list_runs output = %#v", listed)
	}
	filter := runs.listFilter
	if filter.RepositoryID == nil || *filter.RepositoryID != repositoryID ||
		filter.TargetID == nil || *filter.TargetID != targetID ||
		filter.InputArtifactID == nil || *filter.InputArtifactID != artifactID ||
		filter.CommitSHA != commitSHA || filter.ClusterName != "homelab" ||
		filter.Phase != run.PhaseFinished || filter.ReviewState != run.ReviewStateApproved ||
		filter.Limit != 2 || filter.Offset != 1 {
		t.Errorf("ListRuns filter = %#v", filter)
	}
}

func TestHTTPListRunsRejectsInvalidFilter(t *testing.T) {
	t.Parallel()

	runs := &recordingRuns{}
	handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, runs, nil)
	session := connectTestClient(t, handler, testAPIKey)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_runs",
		Arguments: map[string]any{"targetId": "not-a-uuid"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() result = %#v, want tool error", result)
	}
	if runs.listCalls != 0 {
		t.Errorf("ListRuns calls = %d, want 0", runs.listCalls)
	}
}

func TestHTTPListRunsReturnsServiceValidationErrors(t *testing.T) {
	t.Parallel()

	runs := &recordingRuns{err: run.ErrInvalidRequest}
	handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, runs, nil)
	session := connectTestClient(t, handler, testAPIKey)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_runs",
		Arguments: map[string]any{"phase": "waiting"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() result = %#v, want tool error", result)
	}
	if runs.listCalls != 1 {
		t.Errorf("ListRuns calls = %d, want 1", runs.listCalls)
	}
}
