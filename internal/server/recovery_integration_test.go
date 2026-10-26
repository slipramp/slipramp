package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store"
)

const (
	recoveryClusterName     = "colo"
	recoveryControllerToken = "server-recovery-controller-token"
	recoveryEnrollmentToken = "server-recovery-enrollment-token"
)

func TestServerRestartRecoversActiveRunIntegration(t *testing.T) {
	cfg := recoveryIntegrationConfig(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	app, err := New(ctx, cfg, logger, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if app != nil {
			app.Close()
		}
	})

	firstRunID, secondRunID := createRecoveryRuns(t, ctx, app.persistence.database)
	productAuthorization := bearerAuthorization(createRecoveryProductAPIKey(t, ctx, app.persistence.database, cfg))
	client := recoveryAPIClient(t, app.httpServer.Handler)
	controllerAuthorization := bearerAuthorization(recoveryControllerToken)

	credentialDigest := sha256.Sum256([]byte(recoveryControllerToken))
	enrollment, err := client.EnrollControllerWithResponse(
		ctx,
		api.ControllerEnrollment{
			ClusterName:      recoveryClusterName,
			CredentialSha256: hex.EncodeToString(credentialDigest[:]),
		},
		bearerAuthorization(recoveryEnrollmentToken),
	)
	if err != nil {
		t.Fatalf("enroll controller: %v", err)
	}
	requireRecoveryStatus(t, enrollment, http.StatusNoContent)

	zero := 0
	assignment, err := client.PollForControllerCommandWithResponse(
		ctx,
		&api.PollForControllerCommandParams{TimeoutSeconds: &zero},
		controllerAuthorization,
	)
	if err != nil {
		t.Fatalf("poll for initial assignment: %v", err)
	}
	requireRecoveryStatus(t, assignment, http.StatusOK)
	if assignment.JSON200 == nil || assignment.JSON200.Assignment == nil ||
		assignment.JSON200.Assignment.ID != firstRunID {
		t.Fatalf("initial assignment = %#v, want Run %s", assignment.JSON200, firstRunID)
	}

	jobUID := "server-recovery-job"
	running, err := client.ReportRunObservationWithResponse(
		ctx,
		firstRunID,
		api.RunObservation{
			Phase:  api.RunObservationPhaseRunning,
			JobUID: &jobUID,
		},
		controllerAuthorization,
	)
	if err != nil {
		t.Fatalf("report running observation: %v", err)
	}
	requireRecoveryStatus(t, running, http.StatusNoContent)

	beforeRestart := []byte("before restart\n")
	appendResponse, err := client.AppendRunLogChunkWithBodyWithResponse(
		ctx,
		firstRunID,
		&api.AppendRunLogChunkParams{OffsetBytes: 0},
		"application/octet-stream",
		bytes.NewReader(beforeRestart),
		controllerAuthorization,
	)
	if err != nil {
		t.Fatalf("append log before restart: %v", err)
	}
	requireRecoveryStatus(t, appendResponse, http.StatusNoContent)

	objectKeys := recoveryLogObjectKeys(t, ctx, app.persistence.database, firstRunID)
	objects := app.persistence.artifactContents
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for key := range objectKeys {
			if err := objects.Delete(cleanupContext, key); err != nil {
				t.Errorf("delete recovery log object %q: %v", key, err)
			}
		}
	})

	app.Close()
	app = nil

	app, err = New(ctx, cfg, logger, "test")
	if err != nil {
		t.Fatalf("New() after restart error = %v", err)
	}
	client = recoveryAPIClient(t, app.httpServer.Handler)

	blocked, err := client.PollForControllerCommandWithResponse(
		ctx,
		&api.PollForControllerCommandParams{TimeoutSeconds: &zero},
		controllerAuthorization,
	)
	if err != nil {
		t.Fatalf("poll while recovered Run is active: %v", err)
	}
	requireRecoveryStatus(t, blocked, http.StatusNoContent)

	cursor, err := client.GetRunLogCursorWithResponse(ctx, firstRunID, controllerAuthorization)
	if err != nil {
		t.Fatalf("get recovered log cursor: %v", err)
	}
	requireRecoveryStatus(t, cursor, http.StatusOK)
	if cursor.JSON200 == nil ||
		cursor.JSON200.NextOffsetBytes != int64(len(beforeRestart)) ||
		cursor.JSON200.State != api.LogCursorStateOpen {
		t.Fatalf("recovered log cursor = %#v", cursor.JSON200)
	}

	afterRestart := []byte("after restart\n")
	appendResponse, err = client.AppendRunLogChunkWithBodyWithResponse(
		ctx,
		firstRunID,
		&api.AppendRunLogChunkParams{OffsetBytes: int64(len(beforeRestart))},
		"application/octet-stream",
		bytes.NewReader(afterRestart),
		controllerAuthorization,
	)
	if err != nil {
		t.Fatalf("append log after restart: %v", err)
	}
	requireRecoveryStatus(t, appendResponse, http.StatusNoContent)
	for key := range recoveryLogObjectKeys(t, ctx, app.persistence.database, firstRunID) {
		objectKeys[key] = struct{}{}
	}

	completeLog := append(bytes.Clone(beforeRestart), afterRestart...)
	logDigest := sha256.Sum256(completeLog)
	encodedLogDigest := hex.EncodeToString(logDigest[:])
	seal, err := client.SealRunLogWithResponse(
		ctx,
		firstRunID,
		api.LogSeal{
			SizeBytes: int64(len(completeLog)),
			Sha256:    encodedLogDigest,
		},
		controllerAuthorization,
	)
	if err != nil {
		t.Fatalf("seal recovered log: %v", err)
	}
	requireRecoveryStatus(t, seal, http.StatusNoContent)

	resumeOffset := int64(len(beforeRestart) + 2)
	lastEventID := fmt.Sprint(resumeOffset)
	streamContext, stopStream := context.WithTimeout(ctx, 10*time.Second)
	streamed, err := client.StreamRunLogWithResponse(
		streamContext, firstRunID, &api.StreamRunLogParams{LastEventID: &lastEventID}, productAuthorization,
	)
	stopStream()
	if err != nil {
		t.Fatalf("reconnect to recovered log: %v", err)
	}
	requireRecoveryStatus(t, streamed, http.StatusOK)
	encodedEvent, err := json.Marshal(api.LogEvent{
		OffsetBytes: resumeOffset, NextOffsetBytes: int64(len(completeLog)), ContentsBase64: afterRestart[2:],
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents := fmt.Sprintf(
		"id: %d\nevent: chunk\ndata: %s\n\nid: %d\nevent: sealed\ndata: {}\n\n",
		len(completeLog), encodedEvent, len(completeLog),
	)
	if string(streamed.Body) != expectedEvents {
		t.Errorf("recovered SSE stream = %q, want %q", streamed.Body, expectedEvents)
	}

	succeeded := api.RunObservationOutcomeSucceeded
	exitCode := int32(0)
	finished, err := client.ReportRunObservationWithResponse(
		ctx,
		firstRunID,
		api.RunObservation{
			Phase:    api.RunObservationPhaseFinished,
			Outcome:  &succeeded,
			JobUID:   &jobUID,
			ExitCode: &exitCode,
		},
		controllerAuthorization,
	)
	if err != nil {
		t.Fatalf("report recovered completion: %v", err)
	}
	requireRecoveryStatus(t, finished, http.StatusNoContent)

	loaded, err := client.GetRunWithResponse(ctx, firstRunID, productAuthorization)
	if err != nil {
		t.Fatalf("get recovered Run: %v", err)
	}
	requireRecoveryStatus(t, loaded, http.StatusOK)
	if loaded.JSON200 == nil || loaded.JSON200.ID != firstRunID ||
		loaded.JSON200.Phase != api.RunPhaseFinished ||
		loaded.JSON200.Outcome == nil ||
		*loaded.JSON200.Outcome != api.RunOutcomeSucceeded ||
		loaded.JSON200.JobUID == nil || *loaded.JSON200.JobUID != jobUID ||
		loaded.JSON200.ExitCode == nil || *loaded.JSON200.ExitCode != 0 {
		t.Fatalf("recovered Run = %#v", loaded.JSON200)
	}

	artifacts, err := client.ListRunArtifactsWithResponse(ctx, firstRunID, productAuthorization)
	if err != nil {
		t.Fatalf("list recovered artifacts: %v", err)
	}
	requireRecoveryStatus(t, artifacts, http.StatusOK)
	logArtifact := recoveryLogArtifact(t, artifacts.JSON200)
	if logArtifact.State != api.ArtifactStateSealed ||
		logArtifact.SizeBytes != int64(len(completeLog)) ||
		logArtifact.Sha256 == nil || *logArtifact.Sha256 != encodedLogDigest {
		t.Fatalf("recovered log artifact = %#v", logArtifact)
	}

	download, err := client.DownloadArtifactWithResponse(ctx, logArtifact.ID, productAuthorization)
	if err != nil {
		t.Fatalf("download recovered log: %v", err)
	}
	requireRecoveryStatus(t, download, http.StatusOK)
	if !bytes.Equal(download.Body, completeLog) {
		t.Errorf("downloaded recovered log = %q, want %q", download.Body, completeLog)
	}

	nextAssignment, err := client.PollForControllerCommandWithResponse(
		ctx,
		&api.PollForControllerCommandParams{TimeoutSeconds: &zero},
		controllerAuthorization,
	)
	if err != nil {
		t.Fatalf("poll after recovered completion: %v", err)
	}
	requireRecoveryStatus(t, nextAssignment, http.StatusOK)
	if nextAssignment.JSON200 == nil || nextAssignment.JSON200.Assignment == nil ||
		nextAssignment.JSON200.Assignment.ID != secondRunID {
		t.Fatalf("assignment after recovery = %#v, want Run %s", nextAssignment.JSON200, secondRunID)
	}
}

func createRecoveryRuns(t *testing.T, ctx context.Context, database *store.Database) (uuid.UUID, uuid.UUID) {
	t.Helper()

	repositoryID := uuid.New()
	revisionID := uuid.New()
	firstRunID := uuid.New()
	secondRunID := uuid.New()
	createdAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	commitSHA := strings.Repeat("a", 40)

	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:             repositoryID,
		Name:           "server-recovery",
		CloneURL:       "https://github.com/example/server-recovery.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("create recovery repository: %v", err)
	}

	runtime := catalog.Runtime{
		APIVersion: catalog.APIVersion,
		Kind:       catalog.RuntimeKind,
		Metadata:   catalog.Metadata{Name: "shell"},
		Spec:       catalog.RuntimeSpec{Image: "alpine:3.22"},
		Source:     catalog.Source{Path: ".slipramp/runtime.yaml", Document: 1},
	}
	target := catalog.Target{
		APIVersion: catalog.APIVersion,
		Kind:       catalog.TargetKind,
		Metadata:   catalog.Metadata{Name: "network"},
		Spec: catalog.TargetSpec{
			Runtime:   "shell",
			Placement: "infrastructure",
			Actions: map[string]catalog.Action{
				"deploy": {Command: []string{"sh", "deploy.sh"}},
			},
		},
		Source: catalog.Source{Path: ".slipramp/target.yaml", Document: 1},
	}
	revision := catalog.Revision{
		ID:        revisionID,
		CommitSHA: commitSHA,
		CreatedAt: createdAt.Add(time.Second),
		Catalog: catalog.Catalog{
			Runtimes: []catalog.Runtime{runtime},
			Targets:  []catalog.Target{target},
		},
	}
	if err := database.RecordRepositoryRefresh(
		ctx,
		repositoryID,
		repository.RefreshResult{
			Commit:      &repository.Commit{SHA: commitSHA},
			AttemptedAt: revision.CreatedAt,
			Revision:    &revision,
		},
	); err != nil {
		t.Fatalf("record recovery catalog: %v", err)
	}

	action, found := target.EffectiveAction("deploy")
	if !found {
		t.Fatal("recovery action is missing")
	}
	snapshot := run.Snapshot{
		Repository: run.RepositorySnapshot{
			Name:     "server-recovery",
			CloneURL: "https://github.com/example/server-recovery.git",
			Branch:   "main",
		},
		Target:  target,
		Action:  action,
		Runtime: runtime,
		Placement: run.PlacementSnapshot{
			Name:         "infrastructure",
			ClusterName:  recoveryClusterName,
			JobNamespace: "slipramp-runs",
		},
		Inputs: map[string]run.ArtifactInput{},
	}
	targetRecord, err := database.GetActiveTargetByName(ctx, repositoryID, "network")
	if err != nil {
		t.Fatalf("get recovery Target: %v", err)
	}
	first := run.Run{
		ID:                firstRunID,
		Phase:             run.PhaseQueued,
		Review:            run.Review{State: run.ReviewStateNotRequired},
		TargetID:          targetRecord.ID,
		RepositoryID:      repositoryID,
		CatalogRevisionID: revisionID,
		CommitSHA:         commitSHA,
		TargetName:        "network",
		ActionName:        "deploy",
		Parameters:        map[string]any{},
		PlacementName:     "infrastructure",
		ClusterName:       recoveryClusterName,
		Source:            run.Source{Type: run.SourceTypeManual},
		RequestDigest:     strings.Repeat("b", 64),
		Snapshot:          snapshot,
		CreatedAt:         createdAt.Add(2 * time.Second),
	}
	if _, err := database.CreateRun(ctx, first, nil); err != nil {
		t.Fatalf("create first recovery Run: %v", err)
	}

	second := first
	second.ID = secondRunID
	second.RequestDigest = strings.Repeat("c", 64)
	second.CreatedAt = first.CreatedAt.Add(time.Second)
	if _, err := database.CreateRun(ctx, second, nil); err != nil {
		t.Fatalf("create second recovery Run: %v", err)
	}

	return firstRunID, secondRunID
}

func recoveryIntegrationConfig(t *testing.T) Config {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	s3Endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if databaseURL == "" || s3Endpoint == "" {
		t.Skip("integration test environment is not configured")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse integration database URL: %v", err)
	}
	adminPool, err := pgxpool.NewWithConfig(t.Context(), adminConfig)
	if err != nil {
		t.Fatalf("open integration database admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	databaseName := "slipramp_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := adminPool.Exec(t.Context(), "CREATE DATABASE "+identifier); err != nil {
		t.Fatalf("create recovery database: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := adminPool.Exec(cleanupContext, "DROP DATABASE "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("drop recovery database: %v", err)
		}
	})

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse integration database URL: %v", err)
	}
	parsedURL.Path = "/" + databaseName

	cfg := DefaultConfig()
	cfg.Database.URL = parsedURL.String()
	cfg.ObjectStore.Endpoint = s3Endpoint
	cfg.ObjectStore.Region = os.Getenv("TEST_S3_REGION")
	cfg.ObjectStore.Bucket = os.Getenv("TEST_S3_BUCKET")
	cfg.ObjectStore.AccessKeyID = os.Getenv("TEST_S3_ACCESS_KEY_ID")
	cfg.ObjectStore.SecretAccessKey = os.Getenv("TEST_S3_SECRET_ACCESS_KEY")
	cfg.ControllerEnrollmentTokens = map[string]string{
		recoveryClusterName: recoveryEnrollmentToken,
	}
	configureIntegrationOIDC(t, &cfg)

	return cfg
}

func createRecoveryProductAPIKey(t *testing.T, ctx context.Context, database *store.Database, cfg Config) string {
	t.Helper()

	service, err := productauth.NewService(database, recoveryOIDCProvider{}, cfg.ProductAuthentication)
	if err != nil {
		t.Fatalf("create recovery product authentication service: %v", err)
	}
	principal, err := service.CreateServiceAccount(ctx, "Recovery integration", productidentity.RoleAdmin)
	if err != nil {
		t.Fatalf("create recovery service account: %v", err)
	}
	credential, err := service.CreateServiceAccountAPIKey(
		ctx,
		principal.ID,
		principal.ID,
		"recovery integration",
		[]productidentity.Scope{productidentity.ScopeAdmin},
		nil,
	)
	if err != nil {
		t.Fatalf("create recovery API key: %v", err)
	}

	return credential.Token
}

type recoveryOIDCProvider struct{}

func (recoveryOIDCProvider) AuthorizationURL(string, string, string) string {
	return "http://slipramp.test/authorize"
}

func (recoveryOIDCProvider) Authenticate(context.Context, string, string, string) (productauth.OIDCProfile, error) {
	return productauth.OIDCProfile{}, errors.New("recovery OIDC provider is not interactive")
}

type recoveryHTTPClient struct {
	handler http.Handler
}

func (c recoveryHTTPClient) Do(request *http.Request) (*http.Response, error) {
	response := httptest.NewRecorder()
	c.handler.ServeHTTP(response, request)
	result := response.Result()
	result.Request = request

	return result, nil
}

func recoveryAPIClient(t *testing.T, handler http.Handler) *api.ClientWithResponses {
	t.Helper()

	client, err := api.NewClientWithResponses(
		"http://slipramp.test",
		api.WithHTTPClient(recoveryHTTPClient{handler: handler}),
	)
	if err != nil {
		t.Fatalf("create recovery API client: %v", err)
	}

	return client
}

func bearerAuthorization(token string) api.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+token)

		return nil
	}
}

type recoveryResponse interface {
	GetBody() []byte
	StatusCode() int
}

func requireRecoveryStatus(t *testing.T, response recoveryResponse, want int) {
	t.Helper()

	if response.StatusCode() != want {
		t.Fatalf("response status = %d, want %d: %s", response.StatusCode(), want, response.GetBody())
	}
}

func recoveryLogObjectKeys(
	t *testing.T,
	ctx context.Context,
	database *store.Database,
	runID uuid.UUID,
) map[string]struct{} {
	t.Helper()

	record, err := database.GetArtifactByRunAndName(ctx, runID, "log")
	if err != nil {
		t.Fatalf("get recovery log artifact: %v", err)
	}
	chunks, err := database.ListLogChunks(ctx, record.ID, 0)
	if err != nil {
		t.Fatalf("list recovery log chunks: %v", err)
	}

	keys := make(map[string]struct{}, len(chunks))
	for _, chunk := range chunks {
		keys[chunk.ObjectKey] = struct{}{}
	}

	return keys
}

func recoveryLogArtifact(t *testing.T, list *api.ArtifactList) api.Artifact {
	t.Helper()

	if list == nil {
		t.Fatal("artifact list response is empty")
	}
	for _, item := range list.Items {
		if item.Kind == api.Log {
			return item
		}
	}

	t.Fatal("recovered Run has no log artifact")

	return api.Artifact{}
}
