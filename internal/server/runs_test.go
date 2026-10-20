package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

func TestCreateRunReturnsQueuedRun(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	principalID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500")
	createdAt := time.Date(2026, time.October, 16, 12, 5, 0, 0, time.UTC)
	runs := &recordingRunService{
		created: run.Run{
			ID:                runID,
			Phase:             run.PhaseQueued,
			RepositoryID:      repositoryID,
			CatalogRevisionID: revisionID,
			CommitSHA:         "0123456789abcdef0123456789abcdef01234567",
			TargetName:        "network",
			ActionName:        "preview",
			Parameters:        map[string]any{"mode": "safe", "refresh": true},
			PlacementName:     "infrastructure",
			ClusterName:       "colo",
			Source:            run.Source{Type: run.SourceTypeManual},
			CreatedBy: &productidentity.ActorSnapshot{
				PrincipalID: principalID,
				Kind:        productidentity.KindUser,
				DisplayName: "Ada Lovelace",
			},
			CreatedAt: createdAt,
		},
	}
	body := bytes.NewBufferString(`{
		"repositoryId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
		"target":"network",
		"action":"preview",
		"parameters":{"mode":"safe","refresh":true},
		"source":{"type":"api"}
	}`)
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/runs",
		body,
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "manual-network-preview")
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		nil,
		nil,
		nil,
		runs,
		nil,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body)
	}
	if runs.request.RepositoryID != repositoryID ||
		runs.request.TargetName != "network" ||
		runs.request.ActionName != "preview" ||
		runs.request.Source.Type != run.SourceTypeManual ||
		runs.request.IdempotencyKey == nil ||
		*runs.request.IdempotencyKey != "manual-network-preview" ||
		runs.request.Parameters["mode"] != "safe" ||
		runs.request.Parameters["refresh"] != true {
		t.Errorf("creation request = %#v", runs.request)
	}

	var result api.Run
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.ID != runID || result.Phase != api.RunPhaseQueued ||
		result.CatalogRevisionID != revisionID || result.Cluster != "colo" ||
		!result.CreatedAt.Equal(createdAt) || result.CreatedBy == nil ||
		result.CreatedBy.PrincipalID != principalID ||
		result.CreatedBy.DisplayName != "Ada Lovelace" {
		t.Errorf("Run response = %#v", result)
	}
	mode, err := result.Parameters["mode"].AsParameterValue0()
	if err != nil || mode != "safe" {
		t.Errorf("mode parameter = %q, error = %v", mode, err)
	}
}

func TestAuthenticatedRunOperationsRecordBrowserActor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	principal := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
		Role:        productidentity.RoleOperator,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	authentication := productidentity.Authentication{
		Principal:    principal,
		Method:       productidentity.AuthenticationMethodWebSession,
		CredentialID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501"),
	}
	authenticationService := &recordingProductAuthenticationService{
		authentication: authentication,
	}
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	runs := &recordingRunService{
		created: run.Run{
			ID: runID, Phase: run.PhaseQueued, Parameters: map[string]any{},
			Source: run.Source{Type: run.SourceTypeManual}, CreatedAt: now,
		},
		canceled: run.Run{
			ID: runID, Phase: run.PhaseRunning, Parameters: map[string]any{},
		},
	}
	httpHandler := newHandlerWithProductAuthentication(
		&recordingClusterService{}, nil, nil, nil, runs, nil, nil,
		authenticationService, nil,
	)

	body := bytes.NewBufferString(`{
		"repositoryId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
		"target":"network",
		"action":"preview"
	}`)
	createRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/runs",
		body,
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.AddCookie(&http.Cookie{Name: "slipramp_session", Value: "session-token"})
	createResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(createResponse, createRequest)

	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", createResponse.Code, http.StatusCreated, createResponse.Body)
	}
	if runs.request.Source.Type != run.SourceTypeManual || runs.request.Actor == nil ||
		runs.request.Actor.ID != principal.ID {
		t.Errorf("authenticated creation request = %#v", runs.request)
	}

	cancelRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/runs/"+runID.String()+"/cancel",
		nil,
	)
	cancelRequest.AddCookie(&http.Cookie{Name: "slipramp_session", Value: "session-token"})
	cancelResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(cancelResponse, cancelRequest)

	if cancelResponse.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want %d: %s", cancelResponse.Code, http.StatusOK, cancelResponse.Body)
	}
	if runs.cancelRequest.Actor == nil || runs.cancelRequest.Actor.ID != principal.ID {
		t.Errorf("authenticated cancellation request = %#v", runs.cancelRequest)
	}
}

func TestConfiguredProductAuthenticationRejectsMissingSession(t *testing.T) {
	t.Parallel()

	httpHandler := newHandlerWithProductAuthentication(
		&recordingClusterService{}, nil, nil, nil, &recordingRunService{}, nil, nil,
		&recordingProductAuthenticationService{}, nil,
	)
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/runs",
		nil,
	)
	response := httptest.NewRecorder()
	httpHandler.ServeHTTP(response, request)

	assertProblem(t, response, http.StatusUnauthorized)
}

func TestCreateRunMapsExpectedFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		wantStatus    int
		privateDetail string
	}{
		{
			name:       "invalid parameters",
			err:        run.ErrInvalidParameters,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing action",
			err:        run.ErrActionNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "idempotency conflict",
			err:        run.ErrIdempotencyConflict,
			wantStatus: http.StatusConflict,
		},
		{
			name:          "persistence failure",
			err:           errors.New("database unavailable"),
			wantStatus:    http.StatusInternalServerError,
			privateDetail: "database unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := bytes.NewBufferString(`{
				"repositoryId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
				"target":"network",
				"action":"preview"
			}`)
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/api/v1/runs",
				body,
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			newHandler(
				&recordingClusterService{},
				nil,
				nil,
				nil,
				&recordingRunService{err: test.err},
				nil,
				nil,
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if test.privateDetail != "" &&
				strings.Contains(response.Body.String(), test.privateDetail) {
				t.Error("internal error details were exposed in the problem response")
			}
		})
	}
}

func TestListRunsReturnsFilteredHistory(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	newestID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	olderID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1203")
	observedAt := time.Now().UTC()
	succeeded := run.OutcomeSucceeded
	runs := &recordingRunService{
		listed: []run.Run{
			{
				ID: newestID, Phase: run.PhaseRunning, RepositoryID: repositoryID,
				TargetName: "network", ActionName: "apply", ClusterName: "colo",
				Parameters: map[string]any{}, LastObservedAt: &observedAt,
			},
			{
				ID: olderID, Phase: run.PhaseFinished, Outcome: &succeeded,
				RepositoryID: repositoryID, TargetName: "network", ActionName: "preview",
				ClusterName: "colo", Parameters: map[string]any{},
			},
		},
	}
	clusterStatus := cluster.Status{
		Name:            "colo",
		ConnectionState: cluster.ConnectionStateConnected,
	}
	clusters := &recordingClusterService{clusters: []cluster.Status{clusterStatus}}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/runs?repositoryId="+repositoryID.String()+"&target=network&limit=12",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(clusters, nil, nil, nil, runs, nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if runs.listFilter.RepositoryID == nil ||
		*runs.listFilter.RepositoryID != repositoryID ||
		runs.listFilter.TargetName != "network" ||
		runs.listFilter.Limit != 12 {
		t.Errorf("list filter = %#v", runs.listFilter)
	}

	var result api.RunList
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Items) != 2 || result.Items[0].ID != newestID || result.Items[1].ID != olderID {
		t.Fatalf("Run list = %#v", result.Items)
	}
	if result.Items[0].ObservationState == nil ||
		*result.Items[0].ObservationState != api.RunObservationStateCurrent {
		t.Errorf("newest observation state = %v", result.Items[0].ObservationState)
	}
	if result.Items[1].Outcome == nil ||
		*result.Items[1].Outcome != api.RunOutcomeSucceeded {
		t.Errorf("older outcome = %v", result.Items[1].Outcome)
	}
}

func TestListRunsRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/runs?limit=101",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		nil,
		nil,
		nil,
		&recordingRunService{},
		nil,
		nil,
	).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusBadRequest)
}

func TestCancelRun(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	requestedAt := time.Date(2026, time.October, 17, 18, 0, 0, 0, time.UTC)
	runs := &recordingRunService{
		canceled: run.Run{
			ID:                      runID,
			Phase:                   run.PhaseRunning,
			CancellationRequestedAt: &requestedAt,
			Parameters:              map[string]any{},
		},
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/runs/"+runID.String()+"/cancel",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		nil,
		nil,
		nil,
		runs,
		nil,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if runs.cancelID != runID || runs.cancelCalls != 1 {
		t.Errorf("cancel request = %s (%d calls)", runs.cancelID, runs.cancelCalls)
	}

	var result api.Run
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.ID != runID || result.Phase != api.RunPhaseRunning ||
		result.CancellationRequestedAt == nil ||
		!result.CancellationRequestedAt.Equal(requestedAt) {
		t.Errorf("Run response = %#v", result)
	}
}

func TestGetRunReportsDerivedObservationState(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	observedAt := time.Now().UTC()
	runs := &recordingRunService{
		loaded: run.Run{
			ID:             runID,
			Phase:          run.PhaseRunning,
			ClusterName:    "colo",
			Parameters:     map[string]any{},
			LastObservedAt: &observedAt,
		},
	}
	clusterObservation := cluster.Observation{Name: "colo"}
	connectedCluster := cluster.Status{
		Observation:     clusterObservation,
		ConnectionState: cluster.ConnectionStateConnected,
	}
	clusters := &recordingClusterService{
		clusters: []cluster.Status{connectedCluster},
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/runs/"+runID.String(),
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(
		clusters,
		nil,
		nil,
		nil,
		runs,
		nil,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}

	var result api.Run
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.ObservationState == nil ||
		*result.ObservationState != api.RunObservationStateCurrent {
		t.Errorf(
			"observation state = %v, want %q",
			result.ObservationState,
			api.RunObservationStateCurrent,
		)
	}
}

func TestCancelRunMapsExpectedFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "missing Run", err: run.ErrRunNotFound, wantStatus: http.StatusNotFound},
		{name: "finished Run", err: run.ErrRunFinished, wantStatus: http.StatusConflict},
		{name: "persistence failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/api/v1/runs/018f90ff-9fd8-7b5f-b7e1-f32ff54c1202/cancel",
				nil,
			)
			response := httptest.NewRecorder()
			newHandler(
				&recordingClusterService{},
				nil,
				nil,
				nil,
				&recordingRunService{cancelError: test.err},
				nil,
				nil,
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if strings.Contains(response.Body.String(), "database unavailable") {
				t.Error("internal error details were exposed in the problem response")
			}
		})
	}
}

type recordingRunService struct {
	request         run.CreateRequest
	created         run.Run
	err             error
	loaded          run.Run
	loadError       error
	nextClusterName string
	next            run.Run
	nextError       error
	nextCalls       int
	observation     run.Observation
	reportError     error
	reportCalls     int
	cancelID        uuid.UUID
	cancelRequest   run.CancelRequest
	canceled        run.Run
	cancelError     error
	cancelCalls     int
	nextCancel      run.Run
	nextCancelError error
	nextCancelCalls int
	listFilter      run.ListFilter
	listed          []run.Run
	listError       error
}

func (s *recordingRunService) ListRuns(
	_ context.Context,
	filter run.ListFilter,
) ([]run.Run, error) {
	s.listFilter = filter

	return s.listed, s.listError
}

func (s *recordingRunService) GetRun(
	_ context.Context,
	_ uuid.UUID,
) (run.Run, error) {
	return s.loaded, s.loadError
}

func (s *recordingRunService) CancelRun(
	_ context.Context,
	request run.CancelRequest,
) (run.Run, error) {
	s.cancelCalls++
	s.cancelID = request.RunID
	s.cancelRequest = request

	return s.canceled, s.cancelError
}

func (s *recordingRunService) NextRunToCancel(
	_ context.Context,
	_ string,
) (run.Run, error) {
	s.nextCancelCalls++
	if s.nextCancelError != nil {
		return run.Run{}, s.nextCancelError
	}
	if s.nextCancel.ID == uuid.Nil {
		return run.Run{}, run.ErrNoRunToCancel
	}

	return s.nextCancel, nil
}

func (s *recordingRunService) NextRunAssignment(
	_ context.Context,
	clusterName string,
) (run.Run, error) {
	s.nextCalls++
	s.nextClusterName = clusterName

	return s.next, s.nextError
}

func (s *recordingRunService) CreateRun(
	_ context.Context,
	request run.CreateRequest,
) (run.Run, error) {
	s.request = request

	return s.created, s.err
}

func (s *recordingRunService) ReportObservation(
	_ context.Context,
	observation run.Observation,
) error {
	s.reportCalls++
	s.observation = observation

	return s.reportError
}
