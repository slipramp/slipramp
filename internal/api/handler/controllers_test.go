package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/controllercommand"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

func TestListClustersReturnsObservedHealth(t *testing.T) {
	t.Parallel()

	firstSeenAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	lastSeenAt := firstSeenAt.Add(time.Hour)
	controllers := &recordingClusterService{
		clusters: []cluster.Status{
			{
				Name:            "colo",
				FirstSeenAt:     firstSeenAt,
				LastSeenAt:      lastSeenAt,
				ConnectionState: cluster.ConnectionStateConnected,
			},
		},
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/clusters", nil)
	response := httptest.NewRecorder()

	newAdminTestHandler(controllers, nil, nil, nil, nil, nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body api.ClusterList
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(body.Items) != 1 {
		t.Fatalf("cluster count = %d, want 1", len(body.Items))
	}

	item := body.Items[0]
	if item.Name != "colo" {
		t.Errorf("cluster name = %q, want %q", item.Name, "colo")
	}

	if item.ConnectionState != api.ClusterConnectionStateConnected {
		t.Errorf(
			"connection state = %q, want %q",
			item.ConnectionState,
			api.ClusterConnectionStateConnected,
		)
	}

	if !item.FirstSeenAt.Equal(firstSeenAt) || !item.LastSeenAt.Equal(lastSeenAt) {
		t.Errorf("observation = (%s, %s), want (%s, %s)", item.FirstSeenAt, item.LastSeenAt, firstSeenAt, lastSeenAt)
	}
}

func TestListClustersReturnsProblemWhenServiceFails(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{
		listError: errors.New("database unavailable"),
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/clusters", nil)
	response := httptest.NewRecorder()

	newAdminTestHandler(controllers, nil, nil, nil, nil, nil, nil).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusInternalServerError)
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Error("internal error details were exposed in the problem response")
	}
}

func TestControllerEnrollmentRegistersCredentialDigest(t *testing.T) {
	t.Parallel()

	identities := &recordingControllerIdentityService{}
	digest := strings.Repeat("a", 64)
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/enroll",
		strings.NewReader(
			`{"clusterName":"colo","credentialSha256":"`+digest+`"}`,
		),
	)
	request.Header.Set("Authorization", "Bearer enrollment-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newAdminTestHandler(
		&recordingClusterService{},
		identities,
		nil,
		nil,
		nil,
		nil,
		map[string]string{"colo": "enrollment-token"},
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body)
	}
	if identities.enrolledCluster != "colo" || identities.enrolledDigest != digest {
		t.Errorf(
			"enrollment = (%q, %q), want (colo, %q)",
			identities.enrolledCluster,
			identities.enrolledDigest,
			digest,
		)
	}
}

func TestControllerEnrollmentRejectsInvalidCredential(t *testing.T) {
	t.Parallel()

	identities := &recordingControllerIdentityService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/enroll",
		strings.NewReader(
			`{"clusterName":"colo","credentialSha256":"`+
				strings.Repeat("a", 64)+`"}`,
		),
	)
	request.Header.Set("Authorization", "Bearer incorrect-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newAdminTestHandler(
		&recordingClusterService{},
		identities,
		nil,
		nil,
		nil,
		nil,
		map[string]string{"colo": "enrollment-token"},
	).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusUnauthorized)
	if identities.enrolledCluster != "" {
		t.Errorf("enrolled cluster = %q, want empty", identities.enrolledCluster)
	}
}

func TestControllerEnrollmentReturnsConflict(t *testing.T) {
	t.Parallel()

	identities := &recordingControllerIdentityService{
		enrollErr: controlleridentity.ErrEnrollmentConflict,
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/enroll",
		strings.NewReader(
			`{"clusterName":"colo","credentialSha256":"`+
				strings.Repeat("a", 64)+`"}`,
		),
	)
	request.Header.Set("Authorization", "Bearer enrollment-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newAdminTestHandler(
		&recordingClusterService{},
		identities,
		nil,
		nil,
		nil,
		nil,
		map[string]string{"colo": "enrollment-token"},
	).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusConflict)
}

func TestControllerPollReturnsNoContentWithoutWaiting(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{}
	commands := &recordingControllerCommands{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newControllerCommandTestHandler(controllers, commands, nil).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if controllers.clusterName != "colo" {
		t.Errorf("cluster name = %q, want %q", controllers.clusterName, "colo")
	}
	if commands.calls != 1 || commands.clusterName != "colo" {
		t.Errorf("command selection calls = %d for %q, want 1 for colo", commands.calls, commands.clusterName)
	}
}

func TestControllerPollReturnsFrozenRunAssignmentImmediately(t *testing.T) {
	t.Parallel()

	record := runAssignmentRecord()
	controllers := &recordingClusterService{}
	commands := &recordingControllerCommands{command: controllercommand.Assignment{Run: record}}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/controller/v1/poll", nil)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newControllerCommandTestHandler(controllers, commands, &recordingArtifactService{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}

	var command api.ControllerCommand
	if err := json.NewDecoder(response.Body).Decode(&command); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if command.Type != api.ControllerCommandTypeRunAssignment {
		t.Errorf("command type = %q", command.Type)
	}
	if command.Assignment == nil {
		t.Fatal("command assignment is nil")
	}
	assignment := *command.Assignment
	if assignment.ID != record.ID ||
		assignment.CatalogRevisionID != record.CatalogRevisionID ||
		assignment.CommitSha != record.CommitSHA {
		t.Errorf("assignment identity = %#v", assignment)
	}
	if assignment.Repository.CloneURL != "ssh://git@example.com/ops.git" ||
		assignment.Repository.CheckoutCredentialName == nil ||
		*assignment.Repository.CheckoutCredentialName != "github-readonly" {
		t.Errorf("repository = %#v", assignment.Repository)
	}
	if assignment.Target.Name != "network" || assignment.Target.Path == nil ||
		*assignment.Target.Path != "infrastructure/network" {
		t.Errorf("target = %#v", assignment.Target)
	}
	if assignment.Action.Name != "preview" ||
		assignment.Action.Runtime != "terraform" ||
		len(assignment.Action.Command) != 3 ||
		assignment.Action.ParameterDefinitions == nil {
		t.Errorf("action = %#v", assignment.Action)
	}
	if assignment.Runtime.Image != "terraform:1" ||
		assignment.Runtime.Credentials == nil {
		t.Errorf("runtime = %#v", assignment.Runtime)
	}
	if assignment.Placement.JobNamespace != "slipramp-runs" ||
		assignment.Placement.Credentials == nil ||
		assignment.Placement.PodTemplate == nil {
		t.Errorf("placement = %#v", assignment.Placement)
	}
	if assignment.ArtifactAccess.Token != "run-token" {
		t.Errorf("artifact access = %#v", assignment.ArtifactAccess)
	}
	if commands.calls != 1 {
		t.Errorf("command selection calls = %d, want 1", commands.calls)
	}
}

func TestControllerPollReturnsCancellation(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	jobUID := "job-uid"
	commands := &recordingControllerCommands{command: controllercommand.Cancellation{
		Run: run.Run{
			ID:     runID,
			Phase:  run.PhaseRunning,
			JobUID: &jobUID,
			Snapshot: run.Snapshot{Placement: run.PlacementSnapshot{
				JobNamespace: "slipramp-runs",
			}},
		},
	}}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newControllerCommandTestHandler(&recordingClusterService{}, commands, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}

	var command api.ControllerCommand
	if err := json.NewDecoder(response.Body).Decode(&command); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if command.Type != api.ControllerCommandTypeCancelRun ||
		command.Cancel == nil || command.Cancel.ID != runID ||
		command.Cancel.JobNamespace != "slipramp-runs" ||
		command.Cancel.JobUID == nil || *command.Cancel.JobUID != jobUID {
		t.Errorf("command = %#v", command)
	}
}

func TestControllerPollReturnsKargoPromotionCreation(t *testing.T) {
	t.Parallel()

	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1960")
	selected := promotion.CreationCommand{
		PromotionID:  promotionID,
		Attempt:      2,
		ExternalName: "external-dns-prod-slipramp-" + promotionID.String(),
		Integration:  "prod",
		Project:      "kargo-core",
		Stage:        "external-dns-prod",
		StageUID:     "stage-uid",
		Freight:      "freight-42",
	}
	commands := &recordingControllerCommands{command: controllercommand.PromotionCreation{Command: selected}}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newControllerCommandTestHandler(&recordingClusterService{}, commands, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	var command api.ControllerCommand
	if err := json.NewDecoder(response.Body).Decode(&command); err != nil {
		t.Fatal(err)
	}
	if command.Type != api.ControllerCommandTypeCreateKargoPromotion || command.Promotion == nil ||
		command.Promotion.ID != promotionID || command.Promotion.Attempt != 2 ||
		command.Promotion.Project != "kargo-core" || command.Promotion.Freight != "freight-42" {
		t.Errorf("command = %#v", command)
	}
}

func TestControllerReportsKargoPromotionDispatch(t *testing.T) {
	t.Parallel()

	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1961")
	dispatches := &recordingPromotionDispatchService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/promotions/"+promotionID.String()+"/dispatch",
		strings.NewReader(`{"attempt":2,"result":"created","kargoName":"stage.01test.freight","kargoUid":"promotion-uid"}`),
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	request.Header.Set("Content-Type", "application/json")
	controllers := &recordingClusterService{}
	response := httptest.NewRecorder()

	newPromotionDispatchTestHandler(controllers, dispatches).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body)
	}
	if dispatches.reportCalls != 1 || dispatches.reportCluster != "colo" ||
		dispatches.report.PromotionID != promotionID || dispatches.report.Attempt != 2 ||
		dispatches.report.KargoName == nil || *dispatches.report.KargoName != "stage.01test.freight" ||
		dispatches.report.KargoUID == nil || *dispatches.report.KargoUID != "promotion-uid" {
		t.Errorf("report = %#v for cluster %q", dispatches.report, dispatches.reportCluster)
	}
	if controllers.calls != 1 {
		t.Errorf("controller contacts = %d, want 1", controllers.calls)
	}
}

func TestControllerPollReturnsKargoPromotionReconciliation(t *testing.T) {
	t.Parallel()

	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1963")
	selected := promotion.CreationReconciliationCommand{
		PromotionID: promotionID,
		Integration: "prod",
		Project:     "kargo-core",
		KargoName:   "external-dns-prod.01test.freight",
		KargoUID:    "promotion-uid",
	}
	commands := &recordingControllerCommands{command: controllercommand.PromotionReconciliation{Command: selected}}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newControllerCommandTestHandler(&recordingClusterService{}, commands, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	var command api.ControllerCommand
	if err := json.NewDecoder(response.Body).Decode(&command); err != nil {
		t.Fatal(err)
	}
	if command.Type != api.ControllerCommandTypeReconcileKargoPromotion ||
		command.PromotionReconciliation == nil || command.PromotionReconciliation.ID != promotionID ||
		command.PromotionReconciliation.KargoName != selected.KargoName ||
		command.PromotionReconciliation.KargoUID != selected.KargoUID {
		t.Errorf("command = %#v", command)
	}
}

func TestControllerReportsKargoPromotionReconciliation(t *testing.T) {
	t.Parallel()

	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1964")
	reconciliations := &recordingPromotionReconciliationService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/promotions/"+promotionID.String()+"/reconciliation",
		strings.NewReader(`{
          "kargoName":"external-dns-prod.01test.freight",
          "kargoUid":"promotion-uid",
          "phase":"Succeeded",
          "diagnostic":"terminal before callback"
        }`),
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	request.Header.Set("Content-Type", "application/json")
	controllers := &recordingClusterService{}
	response := httptest.NewRecorder()

	newPromotionReconciliationTestHandler(controllers, reconciliations).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body)
	}
	if reconciliations.reportCalls != 1 || reconciliations.reportCluster != "colo" ||
		reconciliations.report.PromotionID != promotionID ||
		reconciliations.report.KargoName != "external-dns-prod.01test.freight" ||
		reconciliations.report.KargoUID != "promotion-uid" ||
		reconciliations.report.Phase != promotion.CreationReconciliationSucceeded ||
		reconciliations.report.Diagnostic != "terminal before callback" {
		t.Errorf("report = %#v for cluster %q", reconciliations.report, reconciliations.reportCluster)
	}
	if controllers.calls != 1 {
		t.Errorf("controller contacts = %d, want 1", controllers.calls)
	}
}

func TestControllerKargoPromotionDispatchMapsFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		token      string
		reportErr  error
		wantStatus int
		wantCalls  int
	}{
		{name: "missing credential", wantStatus: http.StatusUnauthorized},
		{name: "invalid report", token: "controller-token", reportErr: promotion.ErrInvalid, wantStatus: 400, wantCalls: 1},
		{name: "wrong cluster", token: "controller-token", reportErr: promotion.ErrPromotionNotFound, wantStatus: 404, wantCalls: 1},
		{name: "identity conflict", token: "controller-token", reportErr: promotion.ErrCreationDispatchConflict, wantStatus: 409, wantCalls: 1},
		{name: "store failure", token: "controller-token", reportErr: errors.New("database unavailable"), wantStatus: 500, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dispatches := &recordingPromotionDispatchService{reportErr: test.reportErr}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/controller/v1/promotions/018f90ff-9fd8-7b5f-b7e1-f32ff54c1961/dispatch",
				strings.NewReader(`{"attempt":1,"result":"created","kargoName":"stage.01test.freight","kargoUid":"promotion-uid"}`),
			)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			newPromotionDispatchTestHandler(&recordingClusterService{}, dispatches).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if dispatches.reportCalls != test.wantCalls {
				t.Errorf("report calls = %d, want %d", dispatches.reportCalls, test.wantCalls)
			}
			if strings.Contains(response.Body.String(), "database unavailable") {
				t.Error("internal error details were exposed")
			}
		})
	}
}

func TestControllerPollRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		authorizations []string
	}{
		{
			name: "missing credential",
		},
		{
			name:           "incorrect credential",
			authorizations: []string{"Bearer incorrect-token"},
		},
		{
			name:           "incorrect scheme",
			authorizations: []string{"Basic controller-token"},
		},
		{
			name: "multiple credentials",
			authorizations: []string{
				"Bearer controller-token",
				"Bearer controller-token",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			controllers := &recordingClusterService{}
			commands := &recordingControllerCommands{}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/controller/v1/poll?timeoutSeconds=0",
				nil,
			)
			for _, authorization := range test.authorizations {
				request.Header.Add("Authorization", authorization)
			}

			response := httptest.NewRecorder()
			newControllerCommandTestHandler(controllers, commands, nil).ServeHTTP(response, request)

			assertProblem(t, response, http.StatusUnauthorized)
			if challenge := response.Header().Get("WWW-Authenticate"); challenge != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want %q", challenge, "Bearer")
			}

			if controllers.calls != 0 || commands.calls != 0 {
				t.Errorf("contact calls = %d, command selection calls = %d; want none", controllers.calls, commands.calls)
			}
		})
	}
}

func TestControllerPollMapsFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		contactError  error
		commandError  error
		wantStatus    int
		wantContacts  int
		wantCommands  int
		privateDetail string
	}{
		{
			name:       "invalid timeout",
			path:       "/controller/v1/poll?timeoutSeconds=31",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:          "contact failure",
			path:          "/controller/v1/poll?timeoutSeconds=0",
			contactError:  errors.New("database unavailable"),
			wantStatus:    http.StatusInternalServerError,
			wantContacts:  1,
			privateDetail: "database unavailable",
		},
		{
			name:          "command selection failure",
			path:          "/controller/v1/poll?timeoutSeconds=0",
			commandError:  errors.New("database unavailable"),
			wantStatus:    http.StatusInternalServerError,
			wantContacts:  1,
			wantCommands:  1,
			privateDetail: "database unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			controllers := &recordingClusterService{err: test.contactError}
			commands := &recordingControllerCommands{err: test.commandError}
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, test.path, nil)
			request.Header.Set("Authorization", "Bearer controller-token")
			response := httptest.NewRecorder()

			newControllerCommandTestHandler(controllers, commands, nil).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if controllers.calls != test.wantContacts || commands.calls != test.wantCommands {
				t.Errorf("contact calls = %d, command selection calls = %d; want %d, %d",
					controllers.calls, commands.calls, test.wantContacts, test.wantCommands)
			}
			if test.privateDetail != "" &&
				strings.Contains(response.Body.String(), test.privateDetail) {
				t.Error("internal error details were exposed in the problem response")
			}
		})
	}
}

func TestControllerAppendsRunLogChunk(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	controllers := &recordingClusterService{}
	artifacts := &recordingArtifactService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/runs/"+runID.String()+"/log-chunks?offsetBytes=12",
		strings.NewReader("hello logs\n"),
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()

	newAdminTestHandler(
		controllers,
		controllerIdentityFor("controller-token", "colo"),
		nil,
		nil,
		nil,
		artifacts,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body)
	}
	if artifacts.calls != 1 || artifacts.request.RunID != runID ||
		artifacts.request.ClusterName != "colo" ||
		artifacts.request.OffsetBytes != 12 ||
		string(artifacts.request.Contents) != "hello logs\n" {
		t.Errorf("append request = %#v", artifacts.request)
	}
	if controllers.calls != 1 || controllers.clusterName != "colo" {
		t.Errorf("controller contacts = %d for %q, want 1 for colo", controllers.calls, controllers.clusterName)
	}
}

func TestControllerRunLogChunkMapsFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		token      string
		appendErr  error
		wantStatus int
		wantCalls  int
	}{
		{
			name:       "missing credential",
			body:       "logs",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "empty chunk",
			token:      "controller-token",
			appendErr:  artifact.ErrInvalid,
			wantStatus: http.StatusBadRequest,
			wantCalls:  1,
		},
		{
			name:       "wrong cluster",
			body:       "logs",
			token:      "controller-token",
			appendErr:  artifact.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantCalls:  1,
		},
		{
			name:       "conflicting chunk",
			body:       "logs",
			token:      "controller-token",
			appendErr:  artifact.ErrChunkConflict,
			wantStatus: http.StatusConflict,
			wantCalls:  1,
		},
		{
			name:       "oversized chunk",
			body:       strings.Repeat("x", artifact.MaxLogChunkSizeBytes+1),
			token:      "controller-token",
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:       "service failure",
			body:       "logs",
			token:      "controller-token",
			appendErr:  errors.New("S3 unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantCalls:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			artifacts := &recordingArtifactService{err: test.appendErr}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/controller/v1/runs/"+
					"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200/log-chunks?offsetBytes=0",
				strings.NewReader(test.body),
			)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			request.Header.Set("Content-Type", "application/octet-stream")
			response := httptest.NewRecorder()

			newAdminTestHandler(
				&recordingClusterService{},
				controllerIdentityFor("controller-token", "colo"),
				nil,
				nil,
				nil,
				artifacts,
				nil,
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if artifacts.calls != test.wantCalls {
				t.Errorf("append calls = %d, want %d", artifacts.calls, test.wantCalls)
			}
			if strings.Contains(response.Body.String(), "S3 unavailable") {
				t.Error("internal error details were exposed in the problem response")
			}
		})
	}
}

func TestControllerFailsIncompleteRunLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		token      string
		failure    error
		wantStatus int
		wantCalls  int
	}{
		{name: "accepted", token: "controller-token", wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "missing credential", wantStatus: http.StatusUnauthorized},
		{
			name:       "invalid request",
			token:      "controller-token",
			failure:    artifact.ErrInvalid,
			wantStatus: http.StatusBadRequest,
			wantCalls:  1,
		},
		{
			name:       "wrong cluster",
			token:      "controller-token",
			failure:    artifact.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantCalls:  1,
		},
		{
			name:       "sealed log",
			token:      "controller-token",
			failure:    artifact.ErrSealConflict,
			wantStatus: http.StatusConflict,
			wantCalls:  1,
		},
		{
			name:       "service failure",
			token:      "controller-token",
			failure:    errors.New("PostgreSQL unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantCalls:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
			artifacts := &recordingArtifactService{failErr: test.failure}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/controller/v1/runs/"+runID.String()+"/log/failure",
				nil,
			)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()

			newAdminTestHandler(
				&recordingClusterService{},
				controllerIdentityFor("controller-token", "colo"),
				nil,
				nil,
				nil,
				artifacts,
				nil,
			).ServeHTTP(response, request)

			if test.wantStatus == http.StatusNoContent {
				if response.Code != test.wantStatus {
					t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body)
				}
			} else {
				assertProblem(t, response, test.wantStatus)
			}
			if artifacts.failCalls != test.wantCalls {
				t.Errorf("failure calls = %d, want %d", artifacts.failCalls, test.wantCalls)
			}
			if test.wantCalls == 1 &&
				(artifacts.failRequest.RunID != runID || artifacts.failRequest.ClusterName != "colo") {
				t.Errorf("failure request = %#v", artifacts.failRequest)
			}
			if strings.Contains(response.Body.String(), "PostgreSQL unavailable") {
				t.Error("internal error details were exposed in the problem response")
			}
		})
	}
}

func TestControllerReportsRunObservation(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	controllers := &recordingClusterService{}
	runs := &recordingRunService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/runs/"+runID.String()+"/observations",
		strings.NewReader(`{
            "phase":"running",
            "jobUid":"job-uid",
            "images":{
                "action":"registry.example.com/tools@sha256:action",
                "checkout":"registry.example.com/checkout@sha256:checkout",
                "harness":"registry.example.com/harness@sha256:harness"
            }
        }`),
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newAdminTestHandler(
		controllers,
		controllerIdentityFor("controller-token", "colo"),
		nil,
		nil,
		runs,
		nil,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body)
	}
	if runs.reportCalls != 1 || runs.observation.ID != runID ||
		runs.observation.ClusterName != "colo" ||
		runs.observation.Phase != run.PhaseRunning ||
		runs.observation.JobUID == nil || *runs.observation.JobUID != "job-uid" ||
		runs.observation.Images.Action == nil ||
		*runs.observation.Images.Action != "registry.example.com/tools@sha256:action" ||
		runs.observation.Images.Checkout == nil ||
		*runs.observation.Images.Checkout != "registry.example.com/checkout@sha256:checkout" ||
		runs.observation.Images.Harness == nil ||
		*runs.observation.Images.Harness != "registry.example.com/harness@sha256:harness" {
		t.Errorf("observation = %#v", runs.observation)
	}
	if controllers.calls != 1 || controllers.clusterName != "colo" {
		t.Errorf("controller contacts = %d for %q, want 1 for colo", controllers.calls, controllers.clusterName)
	}
}

func TestControllerRunObservationMapsFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reportErr  error
		token      string
		wantStatus int
		wantCalls  int
	}{
		{
			name:       "missing credential",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid observation",
			reportErr:  run.ErrInvalidRequest,
			token:      "controller-token",
			wantStatus: http.StatusBadRequest,
			wantCalls:  1,
		},
		{
			name:       "wrong cluster",
			reportErr:  run.ErrRunNotFound,
			token:      "controller-token",
			wantStatus: http.StatusNotFound,
			wantCalls:  1,
		},
		{
			name:       "conflicting observation",
			reportErr:  run.ErrObservationConflict,
			token:      "controller-token",
			wantStatus: http.StatusConflict,
			wantCalls:  1,
		},
		{
			name:       "service failure",
			reportErr:  errors.New("database unavailable"),
			token:      "controller-token",
			wantStatus: http.StatusInternalServerError,
			wantCalls:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runs := &recordingRunService{reportError: test.reportErr}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/controller/v1/runs/"+
					"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200/observations",
				strings.NewReader(`{"phase":"running"}`),
			)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			newAdminTestHandler(
				&recordingClusterService{},
				controllerIdentityFor("controller-token", "colo"),
				nil,
				nil,
				runs,
				nil,
				nil,
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if runs.reportCalls != test.wantCalls {
				t.Errorf("report calls = %d, want %d", runs.reportCalls, test.wantCalls)
			}
		})
	}
}

type recordingClusterService struct {
	calls       int
	clusterName string
	err         error
	clusters    []cluster.Status
	listError   error
}

type recordingControllerIdentityService struct {
	clusterByToken  map[string]string
	authenticateErr error
	enrollErr       error
	enrolledCluster string
	enrolledDigest  string
}

type recordingArtifactService struct {
	calls        int
	request      artifact.AppendLogChunkRequest
	err          error
	stream       artifact.LogStream
	streamCalls  int
	streamRunID  uuid.UUID
	streamOffset int64
	failCalls    int
	failRequest  artifact.FailLogRequest
	failErr      error
	storageUsage artifact.StorageUsage
	storageErr   error
}

type recordingPromotionDispatchService struct {
	report        promotion.CreationDispatchReport
	reportErr     error
	reportCalls   int
	reportCluster string
}

type recordingPromotionReconciliationService struct {
	report        promotion.CreationReconciliationReport
	reportErr     error
	reportCalls   int
	reportCluster string
}

type recordingControllerCommands struct {
	command     controllercommand.Command
	err         error
	calls       int
	clusterName string
}

func (s *recordingControllerCommands) Next(_ context.Context, clusterName string) (controllercommand.Command, error) {
	s.calls++
	s.clusterName = clusterName

	return s.command, s.err
}

func newControllerCommandTestHandler(
	clusters ClusterService,
	commands ControllerCommandService,
	artifacts ArtifactService,
) http.Handler {
	return NewHTTP(Dependencies{
		Clusters:             clusters,
		ControllerIdentities: controllerIdentityFor("controller-token", "colo"),
		ControllerCommands:   commands,
		Artifacts:            artifacts,
	}, HTTPOptions{})
}

func (s *recordingPromotionDispatchService) Report(
	_ context.Context,
	clusterName string,
	report promotion.CreationDispatchReport,
) error {
	s.reportCalls++
	s.reportCluster = clusterName
	s.report = report
	return s.reportErr
}

func (s *recordingPromotionReconciliationService) Report(
	_ context.Context,
	clusterName string,
	report promotion.CreationReconciliationReport,
) error {
	s.reportCalls++
	s.reportCluster = clusterName
	s.report = report
	return s.reportErr
}

func newPromotionDispatchTestHandler(
	clusters ClusterService,
	dispatches PromotionCreationDispatchService,
) http.Handler {
	identities := controllerIdentityFor("controller-token", "colo")
	return NewHTTP(
		Dependencies{
			Clusters:             clusters,
			ControllerIdentities: identities,
			PromotionDispatches:  dispatches,
		},
		HTTPOptions{},
	)
}

func newPromotionReconciliationTestHandler(
	clusters ClusterService,
	reconciliations PromotionCreationReconciliationService,
) http.Handler {
	return NewHTTP(
		Dependencies{
			Clusters:                 clusters,
			ControllerIdentities:     controllerIdentityFor("controller-token", "colo"),
			PromotionReconciliations: reconciliations,
		},
		HTTPOptions{},
	)
}

func (r *recordingArtifactService) AppendLogChunk(_ context.Context, request artifact.AppendLogChunkRequest) error {
	r.calls++
	r.request = request

	return r.err
}

func (*recordingArtifactService) GetLogCursor(context.Context, uuid.UUID, string) (artifact.LogCursor, error) {
	return artifact.LogCursor{}, artifact.ErrNotFound
}

func (r *recordingArtifactService) FailLog(_ context.Context, request artifact.FailLogRequest) error {
	r.failCalls++
	r.failRequest = request

	return r.failErr
}

func (*recordingArtifactService) SealLog(context.Context, artifact.SealLogRequest) error {
	return artifact.ErrNotFound
}

func (*recordingArtifactService) RunToken(uuid.UUID) string { return "run-token" }

func (*recordingArtifactService) AuthenticateRunToken(uuid.UUID, string) bool {
	return false
}

func (*recordingArtifactService) Upload(context.Context, artifact.UploadRequest) error {
	return artifact.ErrNotFound
}

func (*recordingArtifactService) List(context.Context, uuid.UUID) ([]artifact.Record, error) {
	return nil, artifact.ErrNotFound
}

func (r *recordingArtifactService) GetStorageUsage(context.Context) (artifact.StorageUsage, error) {
	return r.storageUsage, r.storageErr
}

func (*recordingArtifactService) OpenArtifact(context.Context, uuid.UUID) (artifact.Content, error) {
	return artifact.Content{}, artifact.ErrNotFound
}

func (*recordingArtifactService) OpenInput(context.Context, uuid.UUID, string) (artifact.Content, error) {
	return artifact.Content{}, artifact.ErrNotFound
}

func (r *recordingArtifactService) StreamLog(_ context.Context, runID uuid.UUID, offset int64) (artifact.LogStream, error) {
	r.streamCalls++
	r.streamRunID = runID
	r.streamOffset = offset
	if r.stream == nil {
		return nil, artifact.ErrNotFound
	}

	return r.stream, nil
}

func controllerIdentityFor(token string, clusterName string) *recordingControllerIdentityService {
	return &recordingControllerIdentityService{
		clusterByToken: map[string]string{token: clusterName},
	}
}

func (r *recordingControllerIdentityService) AuthenticateController(
	_ context.Context,
	bearerToken string,
) (controlleridentity.Principal, error) {
	if r.authenticateErr != nil {
		return controlleridentity.Principal{}, r.authenticateErr
	}

	clusterName, ok := r.clusterByToken[bearerToken]
	if !ok {
		return controlleridentity.Principal{}, controlleridentity.ErrUnauthorized
	}

	return controlleridentity.Principal{ClusterName: clusterName}, nil
}

func (r *recordingControllerIdentityService) EnrollController(
	_ context.Context,
	clusterName string,
	credentialSHA256 string,
) error {
	r.enrolledCluster = clusterName
	r.enrolledDigest = credentialSHA256

	return r.enrollErr
}

func (r *recordingClusterService) RecordControllerContact(_ context.Context, clusterName string) error {
	r.calls++
	r.clusterName = clusterName

	return r.err
}

func (r *recordingClusterService) ListClusters(context.Context) ([]cluster.Status, error) {
	return r.clusters, r.listError
}

func runAssignmentRecord() run.Run {
	checkoutCredentialName := "github-readonly"
	podTemplate := &corev1.PodTemplateSpec{}
	podTemplate.Labels = map[string]string{"isolation": "sandboxed"}

	return run.Run{
		ID:                uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200"),
		CatalogRevisionID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201"),
		CommitSHA:         strings.Repeat("a", 40),
		Parameters: map[string]any{
			"refresh": true,
		},
		Snapshot: run.Snapshot{
			Repository: run.RepositorySnapshot{
				Name:                   "ops",
				CloneURL:               "ssh://git@example.com/ops.git",
				Branch:                 "main",
				CheckoutCredentialName: &checkoutCredentialName,
			},
			Target: catalog.Target{
				Metadata: catalog.Metadata{Name: "network"},
				Spec: catalog.TargetSpec{
					Path: "infrastructure/network",
				},
			},
			Action: catalog.EffectiveAction{
				Name:      "preview",
				Intent:    "preview",
				Command:   []string{"terraform", "plan", "-out=plan.tfplan"},
				Runtime:   "terraform",
				Placement: "infrastructure-admin",
				Credentials: map[string]string{
					"backend": "terraform-backend",
				},
				Timeout: "20m",
				Parameters: map[string]catalog.Parameter{
					"refresh": {
						Type:        catalog.ParameterTypeBoolean,
						Environment: "TF_REFRESH",
					},
				},
				Artifacts: map[string]catalog.Artifact{
					"plan": {Path: "/slipramp/artifacts/plan.tfplan"},
				},
			},
			Runtime: catalog.Runtime{
				Metadata: catalog.Metadata{Name: "terraform"},
				Spec: catalog.RuntimeSpec{
					Image: "terraform:1",
					Credentials: map[string]catalog.RuntimeCredential{
						"backend": {
							Inject: catalog.CredentialInjection{
								EnvironmentFrom: true,
							},
						},
					},
				},
			},
			Placement: run.PlacementSnapshot{
				Name:         "infrastructure-admin",
				ClusterName:  "colo",
				JobNamespace: "slipramp-runs",
				Credentials: map[string]run.CredentialSnapshot{
					"terraform-backend": {SecretName: "terraform-backend"},
				},
				PodTemplate: podTemplate,
			},
		},
	}
}
