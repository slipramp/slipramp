package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cli"
)

func TestTargetGetTextAndJSON(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/targets/"+testTargetID {
			http.NotFound(writer, request)

			return
		}
		response := runTargetResponse()
		definition := response["definition"].(map[string]any)
		definition["metadata"] = map[string]any{"name": "storage", "title": "Storage infrastructure"}
		definition["status"] = map[string]any{
			"desiredCommit":  map[string]any{"sha": strings.Repeat("b", 40)},
			"reconciliation": map[string]any{"comparison": "different"},
		}
		spec := definition["spec"].(map[string]any)
		spec["path"] = "terraform/storage"
		spec["runtime"] = "terraform"
		spec["placement"] = "admin"
		spec["timeout"] = "30m"
		spec["watch"] = []string{"terraform/storage/**"}
		writeJSON(t, writer, http.StatusOK, response)
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(t, context.Background(), server, "target", "get", testTargetID)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("text exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"Target: storage (" + testTargetID + ")", "Storage infrastructure", "Repository: infrastructure",
		"State: active", "Reconciliation: different", "terraform/storage", "terraform", "admin", "30m",
		"terraform/storage/**", "Actions: preview", "/targets/" + testTargetID,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("text output missing %q: %q", want, output)
		}
	}

	code, output, stderr = runRemoteApp(
		t, context.Background(), server, "target", "get", testTargetID, "--format", "json",
	)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("JSON exit = %d, stderr = %q", code, stderr)
	}
	var target api.Target
	if err := json.Unmarshal([]byte(output), &target); err != nil {
		t.Fatal(err)
	}
	if target.ID.String() != testTargetID || target.Definition.Metadata.Name != "storage" {
		t.Errorf("JSON Target = %#v", target)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2", requests)
	}
}

func TestTargetExplainTextAndJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet ||
			request.URL.Path != "/api/v1/targets/"+testTargetID+"/actions/preview/explanation" {
			http.NotFound(writer, request)

			return
		}
		writeJSON(t, writer, http.StatusOK, targetExplanationResponse())
	}))
	defer server.Close()

	arguments := []string{"target", "explain", testTargetID, "preview"}
	code, output, stderr := runRemoteApp(t, context.Background(), server, arguments...)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("text exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"Target Action: storage/preview", "Validation: deployment", "Final argv: [\"terraform\",\"plan\"]",
		"Parameter mode: omitted", "Placement: admin", "Cluster: colo", "value from Action storage/preview",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("text output missing %q: %q", want, output)
		}
	}

	arguments = append(arguments, "--format", "json")
	code, output, stderr = runRemoteApp(t, context.Background(), server, arguments...)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("JSON exit = %d, stderr = %q", code, stderr)
	}
	var explanation catalog.Explanation
	if err := json.Unmarshal([]byte(output), &explanation); err != nil {
		t.Fatal(err)
	}
	if explanation.Target.Value.Name != "storage" || explanation.Action.Value != "preview" ||
		explanation.Placement.Cluster.Value != "colo" {
		t.Errorf("JSON explanation = %#v", explanation)
	}
}

func targetExplanationResponse() catalog.Explanation {
	targetOrigin := catalog.Origin{Kind: "target-declaration", Name: "storage", FieldPath: "metadata"}
	actionOrigin := catalog.Origin{
		Kind: "action-declaration", Name: "storage/preview", FieldPath: "spec.actions.preview",
	}
	runtimeOrigin := catalog.Origin{Kind: "runtime-declaration", Name: "terraform", FieldPath: "spec.image"}
	placementOrigin := catalog.Origin{Kind: "placement-default", Name: "admin", FieldPath: "cluster"}

	return catalog.Explanation{
		SchemaVersion: 1,
		Level:         catalog.ValidationLevelDeployment,
		Target: catalog.ExplainedValue[catalog.Metadata]{
			Value:    catalog.Metadata{Name: "storage", Title: "Storage infrastructure"},
			Resolved: true, Provenance: []catalog.Origin{targetOrigin},
		},
		Action: catalog.ExplainedValue[string]{
			Value: "preview", Resolved: true, Provenance: []catalog.Origin{actionOrigin},
		},
		ActionTitle:            catalog.ExplainedValue[string]{Value: "Preview", Resolved: true},
		ActionDescription:      catalog.ExplainedValue[string]{Resolved: true},
		ActionDocumentationURL: catalog.ExplainedValue[string]{Resolved: true},
		Intent:                 catalog.ExplainedValue[string]{Value: "preview", Resolved: true},
		Process: catalog.ProcessExplanation{
			RepositoryMount:  catalog.ExplainedValue[string]{Value: "/workspace", Resolved: true},
			TargetPath:       catalog.ExplainedValue[string]{Value: "terraform/storage", Resolved: true},
			WorkingDirectory: catalog.ExplainedValue[string]{Value: "/workspace/terraform/storage", Resolved: true},
			Command:          catalog.ExplainedValue[[]string]{Value: []string{"terraform", "plan"}, Resolved: true},
			Argv: catalog.ExplainedValue[[]string]{
				Value: []string{"terraform", "plan"}, Resolved: true,
				Provenance: []catalog.Origin{actionOrigin},
			},
			Environment: map[string]catalog.ExplainedValue[string]{},
		},
		Parameters: []catalog.ParameterExplanation{{
			Name: "mode",
			Declaration: catalog.ExplainedValue[catalog.Parameter]{
				Value:    catalog.Parameter{Type: catalog.ParameterTypeEnum, Required: true, Values: []string{"safe", "fast"}},
				Resolved: true, Provenance: []catalog.Origin{actionOrigin},
			},
			Delivery: catalog.ExplainedValue[catalog.ParameterDelivery]{
				Value:    catalog.ParameterDelivery{Arguments: []string{}, Environment: map[string]string{}},
				Resolved: true, Provenance: []catalog.Origin{actionOrigin},
			},
		}},
		Runtime: catalog.RuntimeExplanation{
			Name: catalog.ExplainedValue[string]{Value: "terraform", Resolved: true},
			Image: catalog.ExplainedValue[string]{
				Value: "hashicorp/terraform:1", Resolved: true, Provenance: []catalog.Origin{runtimeOrigin},
			},
			WorkingDirectory: catalog.ExplainedValue[string]{Value: "/workspace", Resolved: true},
			Environment:      map[string]catalog.ExplainedValue[string]{},
		},
		Placement: catalog.PlacementExplanation{
			Name:                   catalog.ExplainedValue[string]{Value: "admin", Resolved: true},
			ConfigurationAvailable: true,
			Cluster: catalog.ExplainedValue[string]{
				Value: "colo", Resolved: true, Provenance: []catalog.Origin{placementOrigin},
			},
			Namespace:      catalog.ExplainedValue[string]{Value: "slipramp-runs", Resolved: true},
			ServiceAccount: catalog.ExplainedValue[string]{Value: "slipramp", Resolved: true},
			NodeSelector:   catalog.ExplainedValue[map[string]string]{Value: map[string]string{}, Resolved: true},
			PodExtensions:  catalog.ExplainedValue[bool]{Resolved: true},
		},
		Timeout: catalog.ExplainedValue[string]{Value: "30m", Resolved: true},
		Resources: catalog.ResourcesExplanation{
			Requests: map[string]catalog.ExplainedValue[string]{}, Limits: map[string]catalog.ExplainedValue[string]{},
			PlacementDefaultsResolved: true,
		},
		Credentials: []catalog.CredentialExplanation{},
		Outputs:     []catalog.OutputExplanation{},
		Inputs:      []catalog.InputExplanation{},
		Watch:       catalog.ExplainedValue[[]string]{Value: []string{"terraform/storage/**"}, Resolved: true},
		Triggers:    []catalog.ExplainedValue[catalog.Trigger]{},
		Diagnostics: catalog.Diagnostics{},
	}
}

func TestTargetListFiltersTextAndJSON(t *testing.T) {
	t.Parallel()

	var requestsMu sync.Mutex
	requests := []url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization = %q", got)
		}
		requestsMu.Lock()
		requests = append(requests, request.URL.Query())
		requestsMu.Unlock()
		writeJSON(t, writer, http.StatusOK, map[string]any{
			"items": []map[string]any{{
				"id": testTargetID, "repositoryId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
				"repositoryName": "infrastructure", "catalogRevisionId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1204",
				"catalogCommit": map[string]any{"sha": strings.Repeat("a", 40)},
				"name":          "storage", "title": "Storage", "actions": []string{"preview", "sync"},
				"runtime": "terraform", "placement": "admin",
				"desiredCommit":  map[string]any{"sha": strings.Repeat("b", 40)},
				"reconciliation": map[string]any{"comparison": "different"},
			}},
			"total": 1, "limit": 10, "offset": 5,
		})
	}))
	defer server.Close()

	arguments := []string{
		"target", "list", "--query", "store", "--repository-id", "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
		"--runtime", "terraform", "--placement", "admin", "--limit", "10", "--offset", "5",
	}
	code, output, stderr := runRemoteApp(t, context.Background(), server, arguments...)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("text exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{testTargetID, "storage", "Storage", "infrastructure", "different", "preview,sync"} {
		if !strings.Contains(output, want) {
			t.Errorf("text output missing %q: %q", want, output)
		}
	}

	arguments = append(arguments, "--format", "json")
	code, output, stderr = runRemoteApp(t, context.Background(), server, arguments...)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("JSON exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(output, `"repositoryName": "infrastructure"`) ||
		!strings.Contains(output, `"total": 1`) {
		t.Errorf("JSON output = %q", output)
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	for _, query := range requests {
		for name, want := range map[string]string{
			"q": "store", "repositoryId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
			"runtime": "terraform", "placement": "admin", "limit": "10", "offset": "5",
		} {
			if got := query.Get(name); got != want {
				t.Errorf("query %s = %q, want %q", name, got, want)
			}
		}
	}
}

func TestTargetListAllPages(t *testing.T) {
	t.Parallel()

	var requestsMu sync.Mutex
	offsets := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/targets" {
			http.NotFound(writer, request)

			return
		}
		offset := request.URL.Query().Get("offset")
		requestsMu.Lock()
		offsets = append(offsets, offset)
		requestsMu.Unlock()
		items := []map[string]any{}
		switch offset {
		case "1":
			items = append(items,
				targetSummaryResponse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202", "storage"),
				targetSummaryResponse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1206", "network"),
			)
		case "3":
			items = append(items, targetSummaryResponse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1207", "compute"))
		default:
			t.Errorf("unexpected offset %q", offset)
		}
		writeJSON(t, writer, http.StatusOK, map[string]any{
			"items": items, "total": 4, "limit": 2, "offset": json.Number(offset),
		})
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"target", "list", "--all", "--limit", "2", "--offset", "1", "--format", "json",
	)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var list api.TargetSummaryList
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 3 || list.Items[0].Name != "storage" || list.Items[2].Name != "compute" ||
		list.Total != 4 || list.Limit != 2 || list.Offset != 1 {
		t.Errorf("Target list = %#v", list)
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if strings.Join(offsets, ",") != "1,3" {
		t.Errorf("offsets = %v, want [1 3]", offsets)
	}
}

func TestTargetListAllRejectsInconsistentPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(t, writer, http.StatusOK, map[string]any{
			"items":  []map[string]any{targetSummaryResponse(testTargetID, "storage")},
			"total":  1,
			"limit":  25,
			"offset": 1,
		})
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(t, context.Background(), server, "target", "list", "--all")
	if code != cli.ExitRemote || output != "" || !strings.Contains(stderr, "requested offset 0 but received offset 1") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}

func TestTargetListAllPreservesLaterPageServerFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("offset") == "1" {
			writeProblem(writer, http.StatusServiceUnavailable, "pagination unavailable")

			return
		}
		writeJSON(t, writer, http.StatusOK, map[string]any{
			"items":  []map[string]any{targetSummaryResponse(testTargetID, "storage")},
			"total":  2,
			"limit":  1,
			"offset": 0,
		})
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t, context.Background(), server, "target", "list", "--all", "--limit", "1",
	)
	if code != cli.ExitRemote || output != "" || !strings.Contains(stderr, "server failure") ||
		!strings.Contains(stderr, "pagination unavailable") || strings.Contains(stderr, "invalid Server response") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}

func targetSummaryResponse(id, name string) map[string]any {
	return map[string]any{
		"id": id, "repositoryId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
		"repositoryName": "infrastructure", "catalogRevisionId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1204",
		"catalogCommit": map[string]any{"sha": strings.Repeat("a", 40)},
		"name":          name, "actions": []string{"preview"},
		"desiredCommit":  map[string]any{"sha": strings.Repeat("b", 40)},
		"reconciliation": map[string]any{"comparison": "unknown"},
	}
}
