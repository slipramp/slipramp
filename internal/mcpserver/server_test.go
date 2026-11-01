package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

const testAPIKey = "slipramp_key_test"

func TestHTTPRequiresAuthentication(t *testing.T) {
	t.Parallel()

	handler := testHTTPHandler(t, testAuthentication(productidentity.ScopeRead), nil, nil, nil)
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

func TestHTTPOAuth(t *testing.T) {
	t.Parallel()

	oauthConfig := OAuthConfig{
		Issuer:   "https://identity.example.com/realms/slipramp",
		Resource: "https://slipramp.example.com/mcp",
	}
	oauthVerifier := &staticOAuthVerifier{verified: VerifiedOAuthAccessToken{
		Credential: productauth.OAuthAccessToken{
			CredentialID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1220"),
			Profile: productauth.OIDCProfile{
				Issuer:  oauthConfig.Issuer,
				Subject: "ada-subject",
			},
			Scopes: []productidentity.Scope{productidentity.ScopeRead},
		},
		Expiration: time.Now().Add(time.Hour),
	}}
	handler, err := NewHTTP(
		Dependencies{
			Authentication:    staticAuthenticator{authentication: testAuthentication()},
			Catalogs:          &recordingCatalogs{},
			Runs:              &recordingRuns{},
			Artifacts:         &recordingArtifacts{},
			OAuthAccessTokens: oauthVerifier,
		},
		Options{ServiceVersion: "test", OAuth: oauthConfig},
	)
	if err != nil {
		t.Fatalf("NewHTTP() error = %v", err)
	}

	metadataRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		ProtectedResourceMetadataURL(oauthConfig.Resource),
		nil,
	)
	metadataResponse := httptest.NewRecorder()
	ProtectedResourceMetadataHandler(oauthConfig).ServeHTTP(metadataResponse, metadataRequest)
	if metadataResponse.Code != http.StatusOK {
		t.Fatalf("metadata status = %d, body = %s", metadataResponse.Code, metadataResponse.Body.String())
	}
	var metadata struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	if err := json.Unmarshal(metadataResponse.Body.Bytes(), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata.Resource != oauthConfig.Resource ||
		!slices.Equal(metadata.AuthorizationServers, []string{oauthConfig.Issuer}) ||
		!slices.Equal(metadata.ScopesSupported, OAuthScopeNames()) {
		t.Errorf("metadata = %#v", metadata)
	}

	unauthorizedRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/mcp",
		bytes.NewBufferString("{}"),
	)
	unauthorizedRequest.Header.Set("Content-Type", "application/json")
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorizedResponse.Code)
	}
	challenge := unauthorizedResponse.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `resource_metadata="`+ProtectedResourceMetadataURL(oauthConfig.Resource)+`"`) {
		t.Errorf("WWW-Authenticate = %q", challenge)
	}

	session := connectTestClient(t, handler, "oauth-access-token")
	callTool(t, session, "list_targets", nil)
	if oauthVerifier.rawToken != "oauth-access-token" {
		t.Errorf("verified token = %q", oauthVerifier.rawToken)
	}
}

func TestHTTPTools(t *testing.T) {
	t.Parallel()

	targetID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1204")
	logID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1205")
	outputID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1206")
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
	runs.listed = run.RunList{Items: []run.Run{runs.record}, Total: 1}
	logContents := []byte("preview output\n")
	artifactContents := []byte(`{"ok":true}`)
	digest := strings.Repeat("a", 64)
	sealedAt := time.Date(2026, time.October, 31, 12, 1, 0, 0, time.UTC)
	provenance := artifact.Provenance{
		ProducerRunID:     runID,
		RepositoryID:      repositoryID,
		CatalogRevisionID: revisionID,
		CommitSHA:         testCommitSHA("b"),
		TargetName:        "dns",
		ActionName:        "preview",
	}
	logArtifact := artifact.Artifact{
		ID: logID, RunID: runID, Name: "log", Kind: artifact.KindLog,
		State: artifact.StateSealed, MediaType: "text/plain; charset=utf-8",
		SizeBytes: int64(len(logContents)), SHA256: &digest,
		RetentionState: artifact.RetentionRetained, CreatedAt: runs.record.CreatedAt, SealedAt: &sealedAt,
	}
	outputArtifact := artifact.Artifact{
		ID: outputID, RunID: runID, Name: "result", Kind: artifact.KindOutput,
		State: artifact.StateSealed, MediaType: "application/json",
		SizeBytes: int64(len(artifactContents)), SHA256: &digest, Sensitive: true,
		RetentionState: artifact.RetentionRetained, CreatedAt: runs.record.CreatedAt, SealedAt: &sealedAt,
	}
	artifacts := &recordingArtifacts{
		records: []artifact.Record{
			{
				Artifact:   logArtifact,
				Provenance: provenance,
			},
			{
				Artifact:    outputArtifact,
				Description: "Preview result.",
				Provenance:  provenance,
			},
		},
		contents: map[uuid.UUID][]byte{outputID: artifactContents},
		log:      logContents,
	}
	handler := testHTTPHandler(t, authentication, catalogs, runs, artifacts)
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
	wantTools := []string{
		"cancel_run",
		"create_run",
		"explain_target_action",
		"get_run",
		"list_run_artifacts",
		"list_runs",
		"list_targets",
		"read_artifact",
		"read_run_log",
	}
	if !slices.Equal(toolNames, wantTools) {
		t.Fatalf("tools = %v, want %v", toolNames, wantTools)
	}

	targetListResult := callTool(t, session, "list_targets", map[string]any{"limit": 10})
	var listed targetListOutput
	decodeStructuredContent(t, targetListResult, &listed)
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

	runListResult := callTool(t, session, "list_runs", map[string]any{
		"targetId": targetID.String(),
		"phase":    "queued",
		"limit":    10,
	})
	var runList runListOutput
	decodeStructuredContent(t, runListResult, &runList)
	if runList.Total != 1 || runList.Limit != 10 || len(runList.Items) != 1 ||
		runList.Items[0].ID != runID.String() || runs.listFilter.TargetID == nil ||
		*runs.listFilter.TargetID != targetID || runs.listFilter.Phase != run.PhaseQueued {
		t.Errorf("list_runs output = %#v, filter = %#v", runList, runs.listFilter)
	}

	got := callTool(t, session, "get_run", map[string]any{"runId": runID.String()})
	if got.IsError || runs.getRunID != runID {
		t.Errorf("get_run result = %#v, requested ID = %s", got, runs.getRunID)
	}
	listedArtifactsResult := callTool(t, session, "list_run_artifacts", map[string]any{"runId": runID.String()})
	var listedArtifacts artifactListOutput
	decodeStructuredContent(t, listedArtifactsResult, &listedArtifacts)
	if len(listedArtifacts.Items) != 2 || listedArtifacts.Items[0].Name != "log" ||
		listedArtifacts.Items[1].ID != outputID.String() || !listedArtifacts.Items[1].Sensitive {
		t.Errorf("list_run_artifacts output = %#v", listedArtifacts)
	}

	logResult := callTool(t, session, "read_run_log", map[string]any{
		"runId":      runID.String(),
		"limitBytes": 7,
	})
	var logPage readRunLogOutput
	decodeStructuredContent(t, logResult, &logPage)
	if logPage.Encoding != "utf-8" || logPage.Contents != "preview" || logPage.NextOffsetBytes != 7 ||
		!logPage.HasMore || logPage.Complete {
		t.Errorf("read_run_log output = %#v", logPage)
	}

	artifactResult := callTool(t, session, "read_artifact", map[string]any{
		"artifactId": outputID.String(),
		"limitBytes": 5,
	})
	var artifactPage readArtifactOutput
	decodeStructuredContent(t, artifactResult, &artifactPage)
	if artifactPage.Encoding != "utf-8" || artifactPage.Contents != `{"ok"` ||
		artifactPage.NextOffsetBytes != 5 || !artifactPage.HasMore {
		t.Errorf("read_artifact output = %#v", artifactPage)
	}

	canceled := callTool(t, session, "cancel_run", map[string]any{"runId": runID.String()})
	if canceled.IsError || runs.cancelRequest.RunID != runID ||
		runs.cancelRequest.Actor == nil || runs.cancelRequest.Actor.ID != authentication.Principal.ID {
		t.Errorf("cancel_run result = %#v, request = %#v", canceled, runs.cancelRequest)
	}
}

func TestHTTPRejectsInvalidCreateRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		authentication productidentity.Authentication
		arguments      map[string]any
	}{
		{
			name:           "missing write scope",
			authentication: testAuthentication(productidentity.ScopeRead),
			arguments: map[string]any{
				"targetId":       "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
				"action":         "preview",
				"idempotencyKey": "denied",
			},
		},
		{
			name:           "malformed Artifact input ID",
			authentication: testAuthentication(productidentity.ScopeRunsWrite),
			arguments: map[string]any{
				"targetId":       "018f90ff-9fd8-7b5f-b7e1-f32ff54c1240",
				"action":         "apply",
				"inputs":         map[string]any{"plan": "not-a-uuid"},
				"idempotencyKey": "invalid-input",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runs := &recordingRuns{}
			handler := testHTTPHandler(t, test.authentication, nil, runs, nil)
			session := connectTestClient(t, handler, testAPIKey)
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      "create_run",
				Arguments: test.arguments,
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
		})
	}
}

func TestHTTPCreateRunArtifactInputs(t *testing.T) {
	t.Parallel()

	targetID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1240")
	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1241")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1242")
	producerRunID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1243")
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1244")
	consumerRunID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1245")
	commitSHA := testCommitSHA("c")
	runs := &recordingRuns{record: run.Run{
		ID:                consumerRunID,
		Phase:             run.PhaseQueued,
		TargetID:          targetID,
		RepositoryID:      repositoryID,
		CatalogRevisionID: revisionID,
		CommitSHA:         commitSHA,
		TargetName:        "dns",
		ActionName:        "apply",
		Parameters:        map[string]any{},
		PlacementName:     "homelab",
		ClusterName:       "homelab",
		Source:            run.Source{Type: run.SourceTypeMCP},
		Review:            run.Review{State: run.ReviewStateNotRequired},
		CreatedAt:         time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC),
		Snapshot: run.Snapshot{Inputs: map[string]run.ArtifactInput{
			"plan": {
				ArtifactID:        artifactID,
				ProducerRunID:     producerRunID,
				Name:              "plan",
				MediaType:         "application/vnd.slipramp.opentofu-plan+json",
				SizeBytes:         2048,
				SHA256:            strings.Repeat("c", 64),
				Sensitive:         true,
				RetentionState:    "retained",
				TargetID:          targetID,
				RepositoryID:      repositoryID,
				CatalogRevisionID: revisionID,
				CommitSHA:         commitSHA,
				TargetName:        "dns",
				ActionName:        "preview",
			},
		}},
	}}
	handler := testHTTPHandler(
		t,
		testAuthentication(productidentity.ScopeRead, productidentity.ScopeRunsWrite),
		nil,
		runs,
		nil,
	)
	session := connectTestClient(t, handler, testAPIKey)

	result := callTool(t, session, "create_run", map[string]any{
		"targetId":       targetID.String(),
		"action":         "apply",
		"inputs":         map[string]any{"plan": artifactID.String()},
		"idempotencyKey": "mcp-apply-plan-1",
	})
	var created runOutput
	decodeStructuredContent(t, result, &created)

	if runs.createRequest.Inputs["plan"] != artifactID {
		t.Errorf("CreateRun inputs = %#v", runs.createRequest.Inputs)
	}
	input, found := created.Inputs["plan"]
	if !found || input.ArtifactID != artifactID.String() ||
		input.ProducerRunID != producerRunID.String() || input.Action != "preview" || !input.Sensitive {
		t.Errorf("create_run input output = %#v", created.Inputs)
	}

	got := callTool(t, session, "get_run", map[string]any{"runId": consumerRunID.String()})
	var fetched runOutput
	decodeStructuredContent(t, got, &fetched)
	if fetched.Inputs["plan"] != input {
		t.Errorf("get_run input output = %#v, want %#v", fetched.Inputs["plan"], input)
	}
}

func TestHTTPSensitiveArtifactAuthorization(t *testing.T) {
	t.Parallel()

	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1220")
	digest := strings.Repeat("b", 64)
	createdAt := time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC)
	authentication := testAuthentication(productidentity.ScopeRead)
	authentication.Principal.Role = productidentity.RoleViewer
	record := artifact.Artifact{
		ID: artifactID, RunID: uuid.New(), Name: "plan", Kind: artifact.KindOutput,
		State: artifact.StateSealed, MediaType: "application/json", SizeBytes: 2,
		SHA256: &digest, Sensitive: true, RetentionState: artifact.RetentionRetained,
		CreatedAt: createdAt, SealedAt: &createdAt,
	}
	artifacts := &recordingArtifacts{
		records:  []artifact.Record{{Artifact: record}},
		contents: map[uuid.UUID][]byte{artifactID: []byte("{}")},
	}
	handler := testHTTPHandler(t, authentication, nil, nil, artifacts)
	session := connectTestClient(t, handler, testAPIKey)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "read_artifact",
		Arguments: map[string]any{"artifactId": artifactID.String()},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() result = %#v, want tool error", result)
	}
}

func TestHTTPRecordsToolCallTelemetry(t *testing.T) {
	t.Parallel()

	targetID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1230")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1231")
	authentication := testAuthentication(productidentity.ScopeRead, productidentity.ScopeRunsWrite)
	runs := &recordingRuns{record: run.Run{
		ID:            runID,
		TargetID:      targetID,
		Parameters:    map[string]any{},
		Source:        run.Source{Type: run.SourceTypeMCP},
		Review:        run.Review{State: run.ReviewStateNotRequired},
		CreatedAt:     time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC),
		Phase:         run.PhaseQueued,
		TargetName:    "dns",
		ActionName:    "preview",
		PlacementName: "homelab",
		ClusterName:   "homelab",
	}}
	observer := &recordingToolCallObserver{}
	var logs bytes.Buffer
	handler, err := NewHTTP(
		Dependencies{
			Authentication:   staticAuthenticator{authentication: authentication},
			Catalogs:         &recordingCatalogs{},
			Runs:             runs,
			Artifacts:        &recordingArtifacts{},
			ToolCallObserver: observer,
		},
		Options{
			ServiceVersion: "test",
			Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		},
	)
	if err != nil {
		t.Fatalf("NewHTTP() error = %v", err)
	}

	session := connectTestClient(t, handler, testAPIKey)
	callTool(t, session, "create_run", map[string]any{
		"targetId":       targetID.String(),
		"action":         "preview",
		"parameters":     map[string]any{"environment": "secret-parameter-marker"},
		"idempotencyKey": "secret-idempotency-marker",
	})
	runs.err = errors.Join(run.ErrInvalidParameters, errors.New("secret-rejected-error-marker"))
	rejected, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "create_run",
		Arguments: map[string]any{
			"targetId":       targetID.String(),
			"action":         "preview",
			"idempotencyKey": "rejected",
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !rejected.IsError {
		t.Fatalf("CallTool() result = %#v, want tool error", rejected)
	}

	if len(observer.calls) != 2 {
		t.Fatalf("observed tool calls = %d, want 2", len(observer.calls))
	}
	observed := observer.calls[0]
	if observed.tool != "create_run" || observed.result != ToolCallSucceeded || observed.duration < 0 {
		t.Errorf("observed tool call = %#v", observed)
	}
	if observer.calls[1].result != ToolCallRejected {
		t.Errorf("rejected tool call = %#v", observer.calls[1])
	}

	contents := logs.String()
	for _, expected := range []string{
		`"msg":"MCP tool call"`,
		`"tool":"create_run"`,
		`"result":"succeeded"`,
		`"principal_id":"` + authentication.Principal.ID.String() + `"`,
		`"principal_kind":"service-account"`,
		`"authentication_method":"api-key"`,
		`"credential_id":"` + authentication.CredentialID.String() + `"`,
		`"client_name":"slipramp-test"`,
		`"client_version":"test"`,
		`"protocol_version":`,
		`"target_id":"` + targetID.String() + `"`,
		`"run_id":"` + runID.String() + `"`,
	} {
		if !strings.Contains(contents, expected) {
			t.Errorf("MCP log does not contain %q: %s", expected, contents)
		}
	}
	for _, forbidden := range []string{
		"server connecting",
		"server session connected",
		"server session disconnected",
		"secret-parameter-marker",
		"secret-idempotency-marker",
		"secret-rejected-error-marker",
	} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("MCP log contains %q: %s", forbidden, contents)
		}
	}
}

func TestClassifyToolCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want ToolCallOutcome
	}{
		{name: "succeeded", want: ToolCallSucceeded},
		{name: "rejected", err: errors.New("invalid request"), want: ToolCallRejected},
		{name: "failed", err: errInternalToolCall, want: ToolCallFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := classifyToolCall(test.err); got != test.want {
				t.Errorf("classifyToolCall() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSDKLoggerLevel(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := sdkLogger(slog.New(slog.NewJSONHandler(&logs, nil)))
	logger.Info("lifecycle noise")
	logger.Warn("protocol warning")
	logger.Error("protocol error")

	contents := logs.String()
	if strings.Contains(contents, "lifecycle noise") {
		t.Errorf("SDK log contains Info lifecycle message: %s", contents)
	}
	for _, expected := range []string{"protocol warning", "protocol error"} {
		if !strings.Contains(contents, expected) {
			t.Errorf("SDK log does not contain %q: %s", expected, contents)
		}
	}
}

func testHTTPHandler(
	t *testing.T,
	authentication productidentity.Authentication,
	catalogs CatalogService,
	runs RunService,
	artifacts ArtifactService,
) http.Handler {
	t.Helper()

	if catalogs == nil {
		catalogs = &recordingCatalogs{}
	}
	if runs == nil {
		runs = &recordingRuns{}
	}
	if artifacts == nil {
		artifacts = &recordingArtifacts{}
	}
	handler, err := NewHTTP(
		Dependencies{
			Authentication: staticAuthenticator{authentication: authentication},
			Catalogs:       catalogs,
			Runs:           runs,
			Artifacts:      artifacts,
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
		encoded, _ := json.Marshal(result.Content)
		t.Fatalf("CallTool(%q) error content = %s", name, encoded)
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

type observedToolCall struct {
	tool     string
	result   ToolCallOutcome
	duration time.Duration
}

type recordingToolCallObserver struct {
	calls []observedToolCall
}

func (o *recordingToolCallObserver) ObserveMCPToolCall(
	_ context.Context,
	tool string,
	result ToolCallOutcome,
	duration time.Duration,
) {
	o.calls = append(o.calls, observedToolCall{tool: tool, result: result, duration: duration})
}

type staticOAuthVerifier struct {
	verified VerifiedOAuthAccessToken
	err      error
	rawToken string
}

func (v *staticOAuthVerifier) VerifyAccessToken(
	_ context.Context,
	rawToken string,
) (VerifiedOAuthAccessToken, error) {
	v.rawToken = rawToken

	return v.verified, v.err
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

func (a staticAuthenticator) AuthenticateOAuthAccessToken(
	_ context.Context,
	token productauth.OAuthAccessToken,
) (productidentity.Authentication, error) {
	if a.err != nil {
		return productidentity.Authentication{}, a.err
	}

	authentication := a.authentication
	authentication.Method = productidentity.AuthenticationMethodOAuthToken
	authentication.CredentialID = token.CredentialID
	authentication.CredentialLabel = "OAuth access token"
	authentication.Scopes = token.Scopes

	return authentication, nil
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
	listed        run.RunList
	err           error
	createCalls   int
	listCalls     int
	createRequest run.CreateRequest
	listFilter    run.RunListFilter
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

func (r *recordingRuns) ListRuns(_ context.Context, filter run.RunListFilter) (run.RunList, error) {
	r.listCalls++
	r.listFilter = filter

	return r.listed, r.err
}

func (r *recordingRuns) CancelRun(_ context.Context, request run.CancelRequest) (run.Run, error) {
	r.cancelRequest = request

	return r.record, r.err
}

type recordingArtifacts struct {
	records  []artifact.Record
	contents map[uuid.UUID][]byte
	log      []byte
	err      error
}

func (a *recordingArtifacts) List(_ context.Context, _ uuid.UUID) ([]artifact.Record, error) {
	return slices.Clone(a.records), a.err
}

func (a *recordingArtifacts) OpenArtifact(_ context.Context, artifactID uuid.UUID) (artifact.Content, error) {
	if a.err != nil {
		return artifact.Content{}, a.err
	}
	for _, record := range a.records {
		if record.ID == artifactID {
			contents, found := a.contents[artifactID]
			if !found {
				return artifact.Content{}, artifact.ErrNotFound
			}

			return artifact.Content{
				Artifact: record.Artifact,
				Body:     io.NopCloser(bytes.NewReader(contents)),
			}, nil
		}
	}

	return artifact.Content{}, artifact.ErrNotFound
}

func (a *recordingArtifacts) StreamLog(
	_ context.Context,
	_ uuid.UUID,
	offset int64,
) (artifact.LogStream, error) {
	if a.err != nil {
		return nil, a.err
	}
	if offset < 0 || offset > int64(len(a.log)) {
		return nil, artifact.ErrInvalid
	}

	return &staticLogStream{offset: offset, contents: a.log[offset:]}, nil
}

type staticLogStream struct {
	offset   int64
	contents []byte
	read     bool
}

func (s *staticLogStream) Next() (artifact.LogStreamChunk, error) {
	if s.read {
		return artifact.LogStreamChunk{}, io.EOF
	}
	s.read = true

	return artifact.LogStreamChunk{
		OffsetBytes:     s.offset,
		NextOffsetBytes: s.offset + int64(len(s.contents)),
		Contents:        slices.Clone(s.contents),
	}, nil
}

func (*staticLogStream) Close() error {
	return nil
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
