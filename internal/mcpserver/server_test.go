package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

const testAPIKey = "slipramp_key_test"

func TestHTTPRequiresSliprampAPIKey(t *testing.T) {
	t.Parallel()

	handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, nil)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", bytes.NewBufferString("{}"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if got := response.Header().Get("WWW-Authenticate"); got != `Bearer realm="slipramp"` {
		t.Errorf("WWW-Authenticate = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != privateNoStore {
		t.Errorf("Cache-Control = %q, want %q", got, privateNoStore)
	}
}

func TestHTTPExposesCatalogAndRunToolsEndToEnd(t *testing.T) {
	t.Parallel()

	targetID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1204")
	authentication := testAuthentication(productidentity.ScopeRead, productidentity.ScopeRunsWrite)
	catalogs := &recordingCatalogs{
		targets: catalog.TargetList{
			Items: []catalog.TargetSummary{{
				ID:                targetID,
				RepositoryID:      repositoryID,
				RepositoryName:    "homelab",
				CatalogRevisionID: revisionID,
				CatalogCommitSHA:  testCommitSHA("a"),
				DesiredCommitSHA:  testCommitSHA("b"),
				Target: catalog.Target{
					Metadata: catalog.Metadata{Name: "dns", Title: "DNS"},
					Spec: catalog.TargetSpec{
						Runtime:   "opentofu",
						Placement: "homelab",
						Actions: map[string]catalog.Action{
							"apply":   {Command: []string{"tofu", "apply"}},
							"preview": {Command: []string{"tofu", "plan"}},
						},
					},
				},
			}},
			Total: 1,
		},
	}
	runs := &recordingRuns{
		record: run.Run{
			ID:                runID,
			Phase:             run.PhaseQueued,
			TargetID:          targetID,
			RepositoryID:      repositoryID,
			CatalogRevisionID: revisionID,
			CommitSHA:         testCommitSHA("b"),
			TargetName:        "dns",
			ActionName:        "preview",
			Parameters:        map[string]any{"environment": "prod"},
			PlacementName:     "homelab",
			ClusterName:       "homelab",
			Source:            run.Source{Type: run.SourceTypeMCP},
			Review:            run.Review{State: run.ReviewStateNotRequired},
			CreatedAt:         time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC),
		},
	}
	handler := testHTTPHandler(t, authentication, catalogs, runs)
	session := connectTestClient(t, handler, testAPIKey)

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	toolNames := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	slices.Sort(toolNames)
	wantTools := []string{"cancel_run", "create_run", "explain_target_action", "get_run", "list_targets"}
	if !slices.Equal(toolNames, wantTools) {
		t.Fatalf("tools = %v, want %v", toolNames, wantTools)
	}

	listResult := callTool(t, session, "list_targets", map[string]any{"limit": 10})
	var listed targetListOutput
	decodeStructuredContent(t, listResult, &listed)
	if listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].ID != targetID.String() ||
		!slices.Equal(listed.Items[0].Actions, []string{"apply", "preview"}) {
		t.Errorf("list_targets output = %#v", listed)
	}
	callTool(t, session, "explain_target_action", map[string]any{
		"targetId": targetID.String(),
		"action":   "preview",
	})

	createResult := callTool(t, session, "create_run", map[string]any{
		"targetId":       targetID.String(),
		"action":         "preview",
		"parameters":     map[string]any{"environment": "prod"},
		"idempotencyKey": "mcp-preview-prod-1",
	})
	var created runOutput
	decodeStructuredContent(t, createResult, &created)
	if created.ID != runID.String() {
		t.Errorf("create_run ID = %q, want %q", created.ID, runID)
	}
	if runs.createRequest.Source.Type != run.SourceTypeMCP ||
		runs.createRequest.Actor == nil ||
		runs.createRequest.Actor.ID != authentication.Principal.ID ||
		runs.createRequest.IdempotencyKey == nil ||
		*runs.createRequest.IdempotencyKey != "mcp-preview-prod-1" {
		t.Errorf("CreateRun request = %#v", runs.createRequest)
	}

	got := callTool(t, session, "get_run", map[string]any{"runId": runID.String()})
	if got.IsError || runs.getRunID != runID {
		t.Errorf("get_run result = %#v, requested ID = %s", got, runs.getRunID)
	}
	canceled := callTool(t, session, "cancel_run", map[string]any{"runId": runID.String()})
	if canceled.IsError || runs.cancelRequest.RunID != runID ||
		runs.cancelRequest.Actor == nil || runs.cancelRequest.Actor.ID != authentication.Principal.ID {
		t.Errorf("cancel_run result = %#v, request = %#v", canceled, runs.cancelRequest)
	}
}

func TestHTTPEnforcesAPIKeyScopesAtToolInvocation(t *testing.T) {
	t.Parallel()

	runs := &recordingRuns{}
	handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, runs)
	session := connectTestClient(t, handler, testAPIKey)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "create_run",
		Arguments: map[string]any{
			"targetId":       "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
			"action":         "preview",
			"idempotencyKey": "denied",
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() result = %#v, want tool error", result)
	}
	if runs.createCalls != 0 {
		t.Errorf("CreateRun calls = %d, want 0", runs.createCalls)
	}
}

func testHTTPHandler(
	t *testing.T,
	authentication productidentity.Authentication,
	catalogs CatalogService,
	runs RunService,
) http.Handler {
	t.Helper()

	if catalogs == nil {
		catalogs = &recordingCatalogs{}
	}
	if runs == nil {
		runs = &recordingRuns{}
	}
	handler, err := NewHTTP(
		Dependencies{
			Authentication: staticAuthenticator{authentication: authentication},
			Catalogs:       catalogs,
			Runs:           runs,
		},
		Options{ServiceVersion: "test"},
	)
	if err != nil {
		t.Fatalf("NewHTTP() error = %v", err)
	}

	return handler
}

func connectTestClient(t *testing.T, handler http.Handler, token string) *mcp.ClientSession {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	httpClient := &http.Client{Transport: handlerTransport{handler: mux, token: token}}
	client := mcp.NewClient(&mcp.Implementation{Name: "slipramp-test", Version: "test"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             "http://slipramp.test/mcp",
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("CallTool(%q) error = %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool(%q) result = %#v", name, result)
	}

	return result
}

func decodeStructuredContent(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()

	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
}

func testAuthentication(scopes ...productidentity.Scope) productidentity.Authentication {
	createdAt := time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)

	return productidentity.Authentication{
		Principal: productidentity.Principal{
			ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1210"),
			Kind:        productidentity.KindServiceAccount,
			DisplayName: "Codex",
			Role:        productidentity.RoleOperator,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		},
		Method:          productidentity.AuthenticationMethodAPIKey,
		CredentialID:    uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1211"),
		CredentialLabel: "Codex MCP",
		Scopes:          scopes,
	}
}

func testCommitSHA(character string) string {
	return character + "000000000000000000000000000000000000000"
}

type staticAuthenticator struct {
	authentication productidentity.Authentication
	err            error
}

func (a staticAuthenticator) AuthenticateAPIKey(
	_ context.Context,
	token string,
) (productidentity.Authentication, error) {
	if a.err != nil {
		return productidentity.Authentication{}, a.err
	}
	if token != testAPIKey {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}

	return a.authentication, nil
}

type recordingCatalogs struct {
	targets     catalog.TargetList
	explanation catalog.Explanation
	err         error
}

func (c *recordingCatalogs) ListTargets(
	_ context.Context,
	_ catalog.TargetListFilter,
) (catalog.TargetList, error) {
	return c.targets, c.err
}

func (c *recordingCatalogs) ExplainAction(
	_ context.Context,
	_ uuid.UUID,
	_ string,
) (catalog.Explanation, error) {
	return c.explanation, c.err
}

type recordingRuns struct {
	record        run.Run
	err           error
	createCalls   int
	createRequest run.CreateRequest
	getRunID      uuid.UUID
	cancelRequest run.CancelRequest
}

func (r *recordingRuns) CreateRun(_ context.Context, request run.CreateRequest) (run.Run, error) {
	r.createCalls++
	r.createRequest = request
	if r.err != nil {
		return run.Run{}, r.err
	}
	record := r.record
	if request.Actor != nil {
		actor, err := request.Actor.ActorSnapshot()
		if err != nil {
			return run.Run{}, err
		}
		record.CreatedBy = &actor
	}
	record.IdempotencyKey = request.IdempotencyKey

	return record, nil
}

func (r *recordingRuns) GetRun(_ context.Context, runID uuid.UUID) (run.Run, error) {
	r.getRunID = runID

	return r.record, r.err
}

func (r *recordingRuns) CancelRun(_ context.Context, request run.CancelRequest) (run.Run, error) {
	r.cancelRequest = request

	return r.record, r.err
}

type handlerTransport struct {
	handler http.Handler
	token   string
}

func (t handlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	cloned.Header.Set("Authorization", "Bearer "+t.token)
	response := httptest.NewRecorder()
	t.handler.ServeHTTP(response, cloned)

	return response.Result(), nil
}
