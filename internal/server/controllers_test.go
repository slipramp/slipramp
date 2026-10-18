package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	"github.com/slipramp/slipramp/internal/controlleridentity"
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

	newHandler(controllers, nil, nil, nil, nil, nil, nil).ServeHTTP(response, request)

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

	newHandler(controllers, nil, nil, nil, nil, nil, nil).ServeHTTP(response, request)

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

	newHandler(
		&recordingClusterService{},
		identities,
		nil,
		nil,
		nil,
		nil,
		map[string]string{"colo": "enrollment-token"},
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d, want %d: %s",
			response.Code,
			http.StatusNoContent,
			response.Body,
		)
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

	newHandler(
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

	newHandler(
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
	runs := &recordingRunService{nextError: run.ErrNoRunAssignment}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		controllerIdentityFor("controller-token", "colo"),
		nil,
		nil,
		runs,
		nil,
		nil,
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
		"/controller/v1/poll",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		controllerIdentityFor("controller-token", "colo"),
		nil,
		nil,
		runs,
		&recordingArtifactService{},
		nil,
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
	if assignment.ArtifactAccess.Token != "run-token" {
		t.Errorf("artifact access = %#v", assignment.ArtifactAccess)
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
		"/controller/v1/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		controllerIdentityFor("controller-token", "colo"),
		nil,
		nil,
		runs,
		nil,
		nil,
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
			newHandler(
				controllers,
				controllerIdentityFor("controller-token", "colo"),
				nil,
				nil,
				nil,
				nil,
				nil,
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
			path:       "/controller/v1/poll?timeoutSeconds=31",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:          "service failure",
			path:          "/controller/v1/poll?timeoutSeconds=0",
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
				controllerIdentityFor("controller-token", "colo"),
				nil,
				nil,
				nil,
				nil,
				nil,
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

	newHandler(
		controllers,
		controllerIdentityFor("controller-token", "colo"),
		nil,
		nil,
		nil,
		artifacts,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d, want %d: %s",
			response.Code,
			http.StatusNoContent,
			response.Body,
		)
	}
	if artifacts.calls != 1 || artifacts.request.RunID != runID ||
		artifacts.request.ClusterName != "colo" ||
		artifacts.request.OffsetBytes != 12 ||
		string(artifacts.request.Contents) != "hello logs\n" {
		t.Errorf("append request = %#v", artifacts.request)
	}
	if controllers.calls != 1 || controllers.clusterName != "colo" {
		t.Errorf(
			"controller contacts = %d for %q, want 1 for colo",
			controllers.calls,
			controllers.clusterName,
		)
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

			newHandler(
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

func TestControllerReportsRunObservation(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	controllers := &recordingClusterService{}
	runs := &recordingRunService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/controller/v1/runs/"+runID.String()+"/observations",
		strings.NewReader(`{"phase":"running","jobUid":"job-uid"}`),
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newHandler(
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
				"/controller/v1/runs/"+
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
	calls   int
	request artifact.AppendLogChunkRequest
	err     error
}

func (r *recordingArtifactService) AppendLogChunk(
	_ context.Context,
	request artifact.AppendLogChunkRequest,
) error {
	r.calls++
	r.request = request

	return r.err
}

func (*recordingArtifactService) GetLogCursor(
	context.Context,
	uuid.UUID,
	string,
) (artifact.LogCursor, error) {
	return artifact.LogCursor{}, artifact.ErrNotFound
}

func (*recordingArtifactService) SealLog(
	context.Context,
	artifact.SealLogRequest,
) error {
	return artifact.ErrNotFound
}

func (*recordingArtifactService) RunToken(uuid.UUID) string { return "run-token" }

func (*recordingArtifactService) AuthenticateRunToken(uuid.UUID, string) bool {
	return false
}

func (*recordingArtifactService) Upload(
	context.Context,
	artifact.UploadRequest,
) error {
	return artifact.ErrNotFound
}

func (*recordingArtifactService) List(
	context.Context,
	uuid.UUID,
) ([]artifact.Record, error) {
	return nil, artifact.ErrNotFound
}

func (*recordingArtifactService) OpenArtifact(
	context.Context,
	uuid.UUID,
) (artifact.Content, error) {
	return artifact.Content{}, artifact.ErrNotFound
}

func (*recordingArtifactService) OpenInput(
	context.Context,
	uuid.UUID,
	string,
) (artifact.Content, error) {
	return artifact.Content{}, artifact.ErrNotFound
}

func (*recordingArtifactService) StreamLog(
	context.Context,
	uuid.UUID,
	int64,
) (io.ReadCloser, error) {
	return nil, artifact.ErrNotFound
}

func controllerIdentityFor(
	token string,
	clusterName string,
) *recordingControllerIdentityService {
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
