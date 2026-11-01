package mcpserver

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

func TestHTTPListRunsFilters(t *testing.T) {
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

func TestHTTPListRunsInvalidFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arguments  map[string]any
		serviceErr error
		wantCalls  int
	}{
		{
			name:      "malformed target ID",
			arguments: map[string]any{"targetId": "not-a-uuid"},
		},
		{
			name:       "invalid domain filter",
			arguments:  map[string]any{"phase": "waiting"},
			serviceErr: run.ErrInvalidRequest,
			wantCalls:  1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runs := &recordingRuns{err: test.serviceErr}
			handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, runs, nil)
			session := connectTestClient(t, handler, testAPIKey)
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      "list_runs",
				Arguments: test.arguments,
			})
			if err != nil {
				t.Fatalf("CallTool() error = %v", err)
			}
			if !result.IsError {
				t.Fatalf("CallTool() result = %#v, want tool error", result)
			}
			if runs.listCalls != test.wantCalls {
				t.Errorf("ListRuns calls = %d, want %d", runs.listCalls, test.wantCalls)
			}
		})
	}
}
