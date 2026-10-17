package server

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
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cluster"
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
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/clusters",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(controllers, nil, nil, nil, nil).ServeHTTP(response, request)

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

	if item.ConnectionState != api.Connected {
		t.Errorf(
			"connection state = %q, want %q",
			item.ConnectionState,
			api.Connected,
		)
	}

	if !item.FirstSeenAt.Equal(firstSeenAt) || !item.LastSeenAt.Equal(lastSeenAt) {
		t.Errorf(
			"observation = (%s, %s), want (%s, %s)",
			item.FirstSeenAt,
			item.LastSeenAt,
			firstSeenAt,
			lastSeenAt,
		)
	}
}

func TestListClustersReturnsProblemWhenServiceFails(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{
		listError: errors.New("database unavailable"),
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/clusters",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(controllers, nil, nil, nil, nil).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusInternalServerError)
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Error("internal error details were exposed in the problem response")
	}
}

func TestControllerPollReturnsNoContentWithoutWaiting(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{}
	runs := &recordingRunService{nextError: run.ErrNoRunAssignment}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/controllers/colo/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		nil,
		nil,
		runs,
		map[string]string{"colo": "controller-token"},
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Errorf(
			"status = %d, want %d",
			response.Code,
			http.StatusNoContent,
		)
	}
	if controllers.clusterName != "colo" {
		t.Errorf(
			"cluster name = %q, want %q",
			controllers.clusterName,
			"colo",
		)
	}
	if runs.nextCalls != 1 || runs.nextClusterName != "colo" {
		t.Errorf(
			"next assignment calls = %d for %q, want 1 for colo",
			runs.nextCalls,
			runs.nextClusterName,
		)
	}
}

func TestControllerPollReturnsFrozenRunAssignmentImmediately(t *testing.T) {
	t.Parallel()

	record := runAssignmentRecord()
	controllers := &recordingClusterService{}
	runs := &recordingRunService{next: record}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/controllers/colo/poll",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		nil,
		nil,
		runs,
		map[string]string{"colo": "controller-token"},
	).ServeHTTP(response, request)

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
	if runs.nextCalls != 1 {
		t.Errorf("next assignment calls = %d, want 1", runs.nextCalls)
	}
}

func TestControllerPollPrioritizesCancelRun(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	jobUID := "job-uid"
	runs := &recordingRunService{
		nextCancel: run.Run{
			ID:     runID,
			Phase:  run.PhaseRunning,
			JobUID: &jobUID,
			Snapshot: run.Snapshot{Placement: run.PlacementSnapshot{
				JobNamespace: "slipramp-runs",
			}},
		},
		next: runAssignmentRecord(),
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/controllers/colo/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		nil,
		nil,
		runs,
		map[string]string{"colo": "controller-token"},
	).ServeHTTP(response, request)

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
	if runs.nextCalls != 0 {
		t.Errorf("next assignment calls = %d, want 0", runs.nextCalls)
	}
}

func TestControllerPollRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusterName    string
		authorizations []string
	}{
		{
			name:        "missing credential",
			clusterName: "colo",
		},
		{
			name:           "incorrect credential",
			clusterName:    "colo",
			authorizations: []string{"Bearer incorrect-token"},
		},
		{
			name:           "unknown cluster",
			clusterName:    "unknown",
			authorizations: []string{"Bearer controller-token"},
		},
		{
			name:           "incorrect scheme",
			clusterName:    "colo",
			authorizations: []string{"Basic controller-token"},
		},
		{
			name:        "multiple credentials",
			clusterName: "colo",
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
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/controller/v1/controllers/"+test.clusterName+
					"/poll?timeoutSeconds=0",
				nil,
			)
			for _, authorization := range test.authorizations {
				request.Header.Add("Authorization", authorization)
			}

			response := httptest.NewRecorder()
			newHandler(
				controllers,
				nil,
				nil,
				nil,
				map[string]string{"colo": "controller-token"},
			).ServeHTTP(response, request)

			assertProblem(t, response, http.StatusUnauthorized)
			if challenge := response.Header().Get("WWW-Authenticate"); challenge != "Bearer" {
				t.Errorf(
					"WWW-Authenticate = %q, want %q",
					challenge,
					"Bearer",
				)
			}

			if controllers.calls != 0 {
				t.Errorf("work wait calls = %d, want 0", controllers.calls)
			}
		})
	}
}

func TestControllerPollMapsFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		serviceError  error
		wantStatus    int
		wantCalls     int
		privateDetail string
	}{
		{
			name:       "invalid timeout",
			path:       "/controller/v1/controllers/colo/poll?timeoutSeconds=31",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:          "service failure",
			path:          "/controller/v1/controllers/colo/poll?timeoutSeconds=0",
			serviceError:  errors.New("database unavailable"),
			wantStatus:    http.StatusInternalServerError,
			wantCalls:     1,
			privateDetail: "database unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			controllers := &recordingClusterService{err: test.serviceError}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				test.path,
				nil,
			)
			request.Header.Set("Authorization", "Bearer controller-token")
			response := httptest.NewRecorder()

			newHandler(
				controllers,
				nil,
				nil,
				nil,
				map[string]string{"colo": "controller-token"},
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if controllers.calls != test.wantCalls {
				t.Errorf(
					"work wait calls = %d, want %d",
					controllers.calls,
					test.wantCalls,
				)
			}
			if test.privateDetail != "" &&
				strings.Contains(response.Body.String(), test.privateDetail) {
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
		"/controller/v1/controllers/colo/runs/"+runID.String()+"/observations",
		strings.NewReader(`{"phase":"running","jobUid":"job-uid"}`),
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		nil,
		nil,
		runs,
		map[string]string{"colo": "controller-token"},
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body)
	}
	if runs.reportCalls != 1 || runs.observation.ID != runID ||
		runs.observation.ClusterName != "colo" ||
		runs.observation.Phase != run.PhaseRunning ||
		runs.observation.JobUID == nil || *runs.observation.JobUID != "job-uid" {
		t.Errorf("observation = %#v", runs.observation)
	}
	if controllers.calls != 1 || controllers.clusterName != "colo" {
		t.Errorf(
			"controller contacts = %d for %q, want 1 for colo",
			controllers.calls,
			controllers.clusterName,
		)
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
				"/controller/v1/controllers/colo/runs/"+
					"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200/observations",
				strings.NewReader(`{"phase":"running"}`),
			)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			newHandler(
				&recordingClusterService{},
				nil,
				nil,
				runs,
				map[string]string{"colo": "controller-token"},
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

func (r *recordingClusterService) RecordControllerContact(
	_ context.Context,
	clusterName string,
) error {
	r.calls++
	r.clusterName = clusterName

	return r.err
}

func (r *recordingClusterService) ListClusters(
	context.Context,
) ([]cluster.Status, error) {
	return r.clusters, r.listError
}

func runAssignmentRecord() run.Run {
	checkoutCredentialName := "github-readonly"
	podTemplate := &corev1.PodTemplateSpec{}
	podTemplate.Labels = map[string]string{"isolation": "sandboxed"}

	return run.Run{
		ID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200"),
		CatalogRevisionID: uuid.MustParse(
			"018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
		),
		CommitSHA: strings.Repeat("a", 40),
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
