package kindacceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
)

func (s *suite) authenticate(t *testing.T) {
	t.Helper()

	t.Log("signing in through OIDC and creating the acceptance API key")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create OIDC cookie jar: %v", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == net.JoinHostPort(s.oidcHost, "5556") {
			address = net.JoinHostPort("127.0.0.1", fmt.Sprint(s.oidcPort))
		}
		return dialer.DialContext(ctx, network, address)
	}

	sessionClient := &http.Client{Jar: jar, Transport: transport}
	loginClient := &http.Client{
		Jar:       jar,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	loginResponse := s.httpRequest(t, loginClient, http.MethodGet, s.webURL+"/api/v1/auth/login", "", nil, nil)
	if loginResponse.status < 300 || loginResponse.status >= 400 {
		t.Fatalf("begin OIDC login returned HTTP %d: %s", loginResponse.status, loginResponse.body)
	}
	authorizationURL := loginResponse.header.Get("Location")
	if authorizationURL == "" {
		t.Fatal("OIDC login response omitted Location")
	}
	parsedAuthorizationURL, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatalf("parse OIDC authorization URL: %v", err)
	}
	query := parsedAuthorizationURL.Query()
	query.Set("connector_id", "mock")
	parsedAuthorizationURL.RawQuery = query.Encode()
	oauthResponse := s.httpRequest(t, sessionClient, http.MethodGet, parsedAuthorizationURL.String(), "", nil, nil)
	if oauthResponse.status < 200 || oauthResponse.status >= 400 {
		t.Fatalf("complete OIDC login returned HTTP %d: %s", oauthResponse.status, oauthResponse.body)
	}

	client, err := api.NewClientWithResponses(s.serverURL, api.WithHTTPClient(sessionClient))
	if err != nil {
		t.Fatalf("create session API client: %v", err)
	}
	principalResponse, err := client.GetCurrentPrincipalWithResponse(t.Context(), nil)
	if err != nil {
		t.Fatalf("get authenticated principal: %v", err)
	}
	requireStatus(t, principalResponse.StatusCode(), principalResponse.Body, http.StatusOK)
	if principalResponse.JSON200 == nil || principalResponse.JSON200.Kind != "user" || principalResponse.JSON200.Role != "admin" {
		t.Fatalf("authenticated principal = %#v, want admin user", principalResponse.JSON200)
	}

	keyResponse, err := client.CreateAPIKeyWithResponse(t.Context(), api.APIKeyCreate{
		Name:   "acceptance automation",
		Scopes: []api.APIKeyScope{"slipramp:admin"},
	})
	if err != nil {
		t.Fatalf("create acceptance API key: %v", err)
	}
	requireStatus(t, keyResponse.StatusCode(), keyResponse.Body, http.StatusCreated)
	if keyResponse.JSON201 == nil || keyResponse.JSON201.Token == "" {
		t.Fatalf("created API key = %#v", keyResponse.JSON201)
	}
	s.productAPIKey = keyResponse.JSON201.Token
	s.productClient = s.newAPIClient(t, s.productAPIKey)
	s.sessionHTTPClient = sessionClient
}

func (s *suite) newAPIClient(t *testing.T, key string) *api.ClientWithResponses {
	t.Helper()

	client, err := api.NewClientWithResponses(
		s.serverURL,
		api.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			request.Header.Set("Authorization", "Bearer "+key)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("create generated API client: %v", err)
	}
	return client
}

func (s *suite) registerRepository(t *testing.T) {
	t.Helper()

	t.Log("registering and refreshing the Git fixture")
	credentialName := "acceptance"
	response, err := s.productClient.CreateRepositoryWithResponse(t.Context(), api.RepositoryConfig{
		Name:                   "acceptance",
		CloneURL:               "ssh://git@acceptance-git.slipramp-system.svc.cluster.local:2222/srv/repository.git",
		Branch:                 "main",
		CommitIndexing:         api.RepositoryCommitIndexingObservedHeads,
		RefreshCredentialName:  &credentialName,
		CheckoutCredentialName: &credentialName,
	})
	if err != nil {
		t.Fatalf("register acceptance repository: %v", err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusCreated)
	if response.JSON201 == nil {
		t.Fatal("register acceptance repository returned no repository")
	}
	s.repositoryID = response.JSON201.ID
	s.waitForRepository(t, s.fixtureCommit)

	catalogResponse, err := s.productClient.GetRepositoryCatalogWithResponse(t.Context(), s.repositoryID)
	if err != nil {
		t.Fatalf("get acceptance catalog: %v", err)
	}
	requireStatus(t, catalogResponse.StatusCode(), catalogResponse.Body, http.StatusOK)
	if catalogResponse.JSON200 == nil {
		t.Fatal("acceptance catalog response is empty")
	}
	s.catalogRevision = *catalogResponse.JSON200
	if s.catalogRevision.Commit.Sha != s.fixtureCommit {
		t.Fatalf("catalog commit = %s, want %s", s.catalogRevision.Commit.Sha, s.fixtureCommit)
	}
	if !containsTargetDefinition(s.catalogRevision.Targets, "acceptance") ||
		!containsRuntime(s.catalogRevision.Runtimes, "acceptance") {
		t.Fatalf("catalog omitted acceptance Target or Runtime: %#v", s.catalogRevision)
	}

	var targets api.TargetSummaryList
	s.mustProductCLIJSON(t, &targets, "target", "list",
		"--repository-id", s.repositoryID.String(),
		"--all", "--limit", "1", "--format", "json")
	if targets.Limit != 1 || targets.Offset != 0 || targets.Total != 2 {
		t.Fatalf("CLI Target pagination = limit %d offset %d total %d, want 1/0/2", targets.Limit, targets.Offset, targets.Total)
	}
	names := make([]string, 0, len(targets.Items))
	for _, target := range targets.Items {
		names = append(names, target.Name)
		if target.Name == "acceptance" {
			s.targetID = target.ID
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"acceptance", "kargo-promotions"}) || s.targetID == uuid.Nil {
		t.Fatalf("CLI Targets = %v, want acceptance and kargo-promotions", names)
	}

	var target api.Target
	s.mustProductCLIJSON(t, &target, "target", "get", s.targetID.String(), "--format", "json")
	if target.ID != s.targetID || target.RepositoryID != s.repositoryID || target.CatalogCommit.Sha != s.fixtureCommit ||
		target.Definition.Metadata.Name != "acceptance" || target.Definition.Spec.Runtime == nil ||
		*target.Definition.Spec.Runtime != "acceptance" || target.Definition.Spec.Placement == nil ||
		*target.Definition.Spec.Placement != "acceptance" {
		t.Fatalf("CLI Target = %#v", target)
	}
	if _, ok := target.Definition.Spec.Actions["preview"]; !ok {
		t.Fatal("CLI Target omitted preview Action")
	}

	var explanation catalog.Explanation
	s.mustProductCLIJSON(t, &explanation,
		"target", "explain", s.targetID.String(), "preview", "--format", "json")
	if explanation.SchemaVersion != 1 || explanation.Level != catalog.ValidationLevelDeployment ||
		explanation.Target.Value.Name != "acceptance" || explanation.Action.Value != "preview" ||
		!explanation.Placement.ConfigurationAvailable || explanation.Placement.Name.Value != "acceptance" ||
		explanation.Placement.Cluster.Value != "kind" {
		t.Fatalf("CLI Target explanation = %#v", explanation)
	}
	for _, parameter := range explanation.Parameters {
		if parameter.Name == "message" && !parameter.Supplied {
			return
		}
	}
	t.Fatal("CLI Target explanation omitted unsupplied message parameter")
}

func containsTargetDefinition(targets []api.TargetDefinition, name string) bool {
	return slices.ContainsFunc(targets, func(target api.TargetDefinition) bool {
		return target.Metadata.Name == name
	})
}

func containsRuntime(runtimes []api.Runtime, name string) bool {
	return slices.ContainsFunc(runtimes, func(runtime api.Runtime) bool {
		return runtime.Metadata.Name == name
	})
}

func (s *suite) runCLI(t *testing.T, key string, args ...string) commandResult {
	t.Helper()

	return s.command(t, nil, []string{
		"SLIPRAMP_URL=" + s.serverURL,
		"SLIPRAMP_API_KEY=" + key,
	}, false, s.cliBin, args...)
}

func (s *suite) mustCLI(t *testing.T, key string, args ...string) []byte {
	t.Helper()

	result := s.runCLI(t, key, args...)
	if result.exitCode != 0 {
		t.Fatalf("CLI failed with exit code %d\nstdout:\n%s\nstderr:\n%s", result.exitCode, result.stdout, result.stderr)
	}
	return result.stdout
}

func (s *suite) mustProductCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	return s.mustCLI(t, s.productAPIKey, args...)
}

func (s *suite) mustCLIJSON(t *testing.T, key string, destination any, args ...string) {
	t.Helper()

	decodeJSON(t, s.mustCLI(t, key, args...), destination)
}

func (s *suite) mustProductCLIJSON(t *testing.T, destination any, args ...string) {
	t.Helper()
	s.mustCLIJSON(t, s.productAPIKey, destination, args...)
}

func (s *suite) createServiceAccount(t *testing.T, displayName, role string) uuid.UUID {
	t.Helper()

	response, err := s.productClient.CreateServiceAccountWithResponse(t.Context(), api.ServiceAccountCreate{
		DisplayName: displayName,
		Role:        role,
	})
	if err != nil {
		t.Fatalf("create service account: %v", err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusCreated)
	if response.JSON201 == nil {
		t.Fatal("create service account returned no account")
	}
	return response.JSON201.PrincipalID
}

func (s *suite) createServiceAccountAPIKey(t *testing.T, principalID uuid.UUID, name string, scopes []string) string {
	t.Helper()

	response, err := s.productClient.CreateServiceAccountAPIKeyWithResponse(t.Context(), principalID, api.APIKeyCreate{
		Name:   name,
		Scopes: scopes,
	})
	if err != nil {
		t.Fatalf("create service-account API key: %v", err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusCreated)
	if response.JSON201 == nil || response.JSON201.Token == "" {
		t.Fatalf("created service-account API key = %#v", response.JSON201)
	}
	return response.JSON201.Token
}

func (s *suite) assertPostForbidden(t *testing.T, key, path string, body any) {
	t.Helper()

	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("encode forbidden request body: %v", err)
		}
	}
	response := s.httpRequest(t, http.DefaultClient, http.MethodPost, s.serverURL+path, key, encoded, nil)
	if response.status != http.StatusForbidden {
		t.Fatalf("POST %s returned HTTP %d, want 403: %s", path, response.status, response.body)
	}
}

func (s *suite) waitForRepository(t *testing.T, expectedCommit string) {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var lastBody []byte
	for time.Now().Before(deadline) {
		response, err := s.productClient.ListRepositoriesWithResponse(t.Context(), nil)
		if err == nil {
			lastBody = response.Body
			if response.JSON200 != nil {
				for _, repository := range response.JSON200.Items {
					if repository.ID == s.repositoryID && repository.Refresh.Status == api.RepositoryRefreshStatusSucceeded &&
						repository.Refresh.Commit != nil && repository.Refresh.Commit.Sha == expectedCommit {
						return
					}
				}
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("repository did not refresh commit %s: %s", expectedCommit, lastBody)
}

func (s *suite) waitForPendingReviewRun(t *testing.T, commit string) api.Run {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	limit := 100
	reviewState := api.RunReviewStatePending
	var lastBody []byte
	for time.Now().Before(deadline) {
		response, err := s.productClient.ListRunsWithResponse(t.Context(), &api.ListRunsParams{
			RepositoryID: &s.repositoryID,
			CommitSha:    &commit,
			ReviewState:  &reviewState,
			Limit:        &limit,
		})
		if err == nil {
			lastBody = response.Body
			if response.JSON200 != nil {
				for _, run := range response.JSON200.Items {
					if run.Commit.Sha == commit && run.Source.Type == api.RunSourceTypeGit && run.Source.Git != nil &&
						run.Source.Git.TriggerName == "review-on-change" && run.Review.State == api.RunReviewStatePending {
						return run
					}
				}
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("pending review Run did not appear for commit %s: %s", commit, lastBody)
	return api.Run{}
}

func stringParameter(t *testing.T, value string) api.ParameterValue {
	t.Helper()

	var parameter api.ParameterValue
	if err := parameter.FromParameterValue0(value); err != nil {
		t.Fatalf("create string parameter: %v", err)
	}
	return parameter
}

func (s *suite) createRun(
	t *testing.T,
	action string,
	parameters map[string]api.ParameterValue,
	inputs map[string]uuid.UUID,
	idempotencyKey string,
) api.Run {
	t.Helper()

	request := api.RunCreate{TargetID: s.targetID, Action: action}
	if parameters != nil {
		request.Parameters = &parameters
	}
	if inputs != nil {
		request.Inputs = &inputs
	}
	response, err := s.productClient.CreateRunWithResponse(
		t.Context(),
		&api.CreateRunParams{IdempotencyKey: &idempotencyKey},
		request,
	)
	if err != nil {
		t.Fatalf("create %s Run: %v", action, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusCreated)
	if response.JSON201 == nil {
		t.Fatalf("create %s Run returned no Run", action)
	}
	return *response.JSON201
}

func (s *suite) getRun(t *testing.T, runID uuid.UUID) api.Run {
	t.Helper()

	response, err := s.productClient.GetRunWithResponse(t.Context(), runID)
	if err != nil {
		t.Fatalf("get Run %s: %v", runID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil {
		t.Fatalf("get Run %s returned no Run", runID)
	}
	return *response.JSON200
}

func (s *suite) cancelRun(t *testing.T, runID uuid.UUID) api.Run {
	t.Helper()

	response, err := s.productClient.CancelRunWithResponse(t.Context(), runID)
	if err != nil {
		t.Fatalf("cancel Run %s: %v", runID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil || response.JSON200.ID != runID {
		t.Fatalf("cancel Run %s response = %#v", runID, response.JSON200)
	}
	return *response.JSON200
}

func (s *suite) waitForRunPhase(t *testing.T, runID uuid.UUID, expected api.RunPhase) api.Run {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var lastBody []byte
	for time.Now().Before(deadline) {
		response, err := s.productClient.GetRunWithResponse(t.Context(), runID)
		if err == nil {
			lastBody = response.Body
			if response.JSON200 != nil {
				run := *response.JSON200
				if run.Phase == expected {
					return run
				}
				if run.Phase == api.RunPhaseFinished {
					t.Fatalf("Run %s finished as %v before reaching %s", runID, run.Outcome, expected)
				}
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("Run %s did not reach phase %s: %s", runID, expected, lastBody)
	return api.Run{}
}

func (s *suite) waitForRunOutcome(t *testing.T, runID uuid.UUID, expected api.RunOutcome) api.Run {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var lastBody []byte
	for time.Now().Before(deadline) {
		response, err := s.productClient.GetRunWithResponse(t.Context(), runID)
		if err == nil {
			lastBody = response.Body
			if response.JSON200 != nil && response.JSON200.Phase == api.RunPhaseFinished {
				if response.JSON200.Outcome == nil || *response.JSON200.Outcome != expected {
					t.Fatalf("Run %s outcome = %v, want %s: %s", runID, response.JSON200.Outcome, expected, response.Body)
				}
				return *response.JSON200
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("Run %s did not finish as %s: %s", runID, expected, lastBody)
	return api.Run{}
}

func (s *suite) updateRepositoryHead(t *testing.T, commit string) {
	t.Helper()

	s.kubectl(t, "-n", systemNamespace, "exec", "deployment/acceptance-git", "-c", "git", "--",
		"git", "--git-dir=/srv/repository.git", "update-ref", "refs/heads/main", commit)
}

func (s *suite) assertPendingReviewNotDispatched(t *testing.T, runID uuid.UUID) {
	t.Helper()

	wait(t, 3*time.Second)
	run := s.getRun(t, runID)
	if run.Phase != api.RunPhaseQueued || run.Review.State != api.RunReviewStatePending || run.JobUID != nil {
		t.Fatalf("pending review Run was dispatched: %#v", run)
	}
	jobs := strings.TrimSpace(string(s.kubectl(t, "-n", runNamespace, "get", "jobs",
		"-l", "slipramp.com/run-id="+runID.String(), "-o", "name")))
	if jobs != "" {
		t.Fatalf("pending review Run %s created Jobs: %s", runID, jobs)
	}
}

func (s *suite) artifact(t *testing.T, runID uuid.UUID, name string) api.Artifact {
	t.Helper()

	response, err := s.productClient.ListRunArtifactsWithResponse(t.Context(), runID)
	if err != nil {
		t.Fatalf("list Run %s artifacts: %v", runID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 != nil {
		for _, artifact := range response.JSON200.Items {
			if artifact.Name == name {
				return artifact
			}
		}
	}
	t.Fatalf("Run %s has no %q artifact: %s", runID, name, response.Body)
	return api.Artifact{}
}

func (s *suite) downloadArtifact(t *testing.T, artifactID uuid.UUID) []byte {
	t.Helper()

	response, err := s.productClient.DownloadArtifactWithResponse(t.Context(), artifactID)
	if err != nil {
		t.Fatalf("download Artifact %s: %v", artifactID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	return response.Body
}

func (s *suite) assertLogContains(t *testing.T, runID uuid.UUID, pattern string) {
	t.Helper()

	logArtifact := s.artifact(t, runID, "log")
	contents := s.downloadArtifact(t, logArtifact.ID)
	if !bytes.Contains(contents, []byte(pattern)) {
		t.Fatalf("Run %s log does not contain %q:\n%s", runID, pattern, contents)
	}
}

func (s *suite) assertCLIWaitOutcome(t *testing.T, runID uuid.UUID, expected api.RunOutcome) {
	t.Helper()

	result := s.runCLI(t, s.productAPIKey,
		"run", "wait", runID.String(),
		"--timeout", s.timeout.String(),
		"--poll-interval", "250ms",
		"--format", "json")
	if result.exitCode != 6 {
		t.Fatalf("CLI wait for Run %s exited %d, want 6\nstdout:\n%s\nstderr:\n%s",
			runID, result.exitCode, result.stdout, result.stderr)
	}
	var run api.Run
	decodeJSON(t, result.stdout, &run)
	if run.ID != runID || run.Phase != api.RunPhaseFinished || run.Outcome == nil || *run.Outcome != expected {
		t.Fatalf("CLI wait Run = %#v", run)
	}
}

func (s *suite) waitForJobDeletion(t *testing.T, runID uuid.UUID) {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	for time.Now().Before(deadline) {
		jobs := strings.TrimSpace(string(s.kubectl(t, "-n", runNamespace, "get", "jobs",
			"-l", "slipramp.com/run-id="+runID.String(), "-o", "name")))
		if jobs == "" {
			return
		}
		wait(t, time.Second)
	}
	t.Fatalf("execution Job for Run %s was not deleted", runID)
}

func (s *suite) assertJobExists(t *testing.T, runID uuid.UUID) {
	t.Helper()

	jobs := strings.TrimSpace(string(s.kubectl(t, "-n", runNamespace, "get", "jobs",
		"-l", "slipramp.com/run-id="+runID.String(), "-o", "name")))
	if jobs == "" {
		t.Fatalf("execution Job for Run %s was deleted before recovery", runID)
	}
}

func (s *suite) assertJobAbsent(t *testing.T, runID uuid.UUID) {
	t.Helper()

	jobs := strings.TrimSpace(string(s.kubectl(t, "-n", runNamespace, "get", "jobs",
		"-l", "slipramp.com/run-id="+runID.String(), "-o", "name")))
	if jobs != "" {
		t.Fatalf("queued Run %s was dispatched during the database outage: %s", runID, jobs)
	}
}

func (s *suite) waitForRunCancellationRequest(t *testing.T, runID uuid.UUID) {
	t.Helper()

	annotationTemplate := `{{range .items}}{{index .metadata.annotations "slipramp.com/cancellation-requested"}}{{end}}`
	deadline := time.Now().Add(s.timeout)
	for time.Now().Before(deadline) {
		value := strings.TrimSpace(string(s.kubectl(t,
			"-n", runNamespace,
			"get", "pods",
			"-l", "slipramp.com/run-id="+runID.String(),
			"-o", "go-template="+annotationTemplate,
		)))
		if value == "true" {
			return
		}
		wait(t, 100*time.Millisecond)
	}
	t.Fatalf("execution Pod for Run %s did not receive its cancellation request", runID)
}

func (s *suite) waitForDurableLogBytes(t *testing.T, runID uuid.UUID, minimum int64) api.Artifact {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var lastBody []byte
	for time.Now().Before(deadline) {
		response, err := s.productClient.ListRunArtifactsWithResponse(t.Context(), runID)
		if err == nil {
			lastBody = response.Body
			if response.JSON200 != nil {
				for _, artifact := range response.JSON200.Items {
					if artifact.Name == "log" && artifact.State == api.ArtifactStateOpen && artifact.SizeBytes >= minimum {
						return artifact
					}
				}
			}
		}
		wait(t, 100*time.Millisecond)
	}
	t.Fatalf("Run %s did not retain at least %d live log bytes: %s", runID, minimum, lastBody)
	return api.Artifact{}
}

func (s *suite) waitForReadiness(
	t *testing.T,
	database api.DependencyStatus,
	artifactStorage api.DependencyStatus,
) api.ReadinessResponse {
	t.Helper()

	expectedStatus := http.StatusServiceUnavailable
	if database == api.Available && artifactStorage == api.Available {
		expectedStatus = http.StatusOK
	}

	deadline := time.Now().Add(s.timeout)
	var lastBody []byte
	for time.Now().Before(deadline) {
		response, err := s.productClient.GetReadinessWithResponse(t.Context())
		if err == nil {
			lastBody = response.Body
			var report *api.ReadinessResponse
			switch expectedStatus {
			case http.StatusOK:
				report = response.JSON200
			case http.StatusServiceUnavailable:
				report = response.JSON503
			}
			if response.StatusCode() == expectedStatus && report != nil &&
				report.Database == database && report.ArtifactStorage == artifactStorage {
				return *report
			}
		}
		wait(t, 250*time.Millisecond)
	}
	t.Fatalf(
		"readiness did not reach database=%q artifact-storage=%q: %s",
		database,
		artifactStorage,
		lastBody,
	)
	return api.ReadinessResponse{}
}

func (s *suite) assertHealthy(t *testing.T) {
	t.Helper()

	response, err := s.productClient.GetHealthWithResponse(t.Context())
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil || response.JSON200.Status != api.Ok {
		t.Fatalf("health response = %#v", response.JSON200)
	}
}

func (s *suite) assertSealedLogEquals(t *testing.T, runID uuid.UUID, expected []byte) {
	t.Helper()

	logArtifact := s.artifact(t, runID, "log")
	if logArtifact.State != api.ArtifactStateSealed || logArtifact.SizeBytes != int64(len(expected)) {
		t.Fatalf("Run %s log metadata = %#v", runID, logArtifact)
	}
	if actual := s.downloadArtifact(t, logArtifact.ID); !bytes.Equal(actual, expected) {
		t.Fatalf("Run %s log = %q, want %q", runID, actual, expected)
	}
}

type httpResult struct {
	status int
	header http.Header
	body   []byte
}

func (s *suite) httpRequest(
	t *testing.T,
	client *http.Client,
	method, requestURL, key string,
	body []byte,
	headers http.Header,
) httpResult {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), method, requestURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create HTTP request: %v", err)
	}
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	if body != nil && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, requestURL, err)
	}
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		_ = response.Body.Close()
		t.Fatalf("read %s %s response: %v", method, requestURL, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close %s %s response: %v", method, requestURL, err)
	}
	return httpResult{status: response.StatusCode, header: response.Header, body: contents}
}

func requireStatus(t *testing.T, actual int, body []byte, expected int) {
	t.Helper()
	if actual != expected {
		t.Fatalf("HTTP status = %d, want %d: %s", actual, expected, body)
	}
}

func decodeJSON(t *testing.T, contents []byte, destination any) {
	t.Helper()
	if err := json.Unmarshal(contents, destination); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, contents)
	}
}
