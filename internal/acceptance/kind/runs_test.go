package kindacceptance

import (
	"bytes"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

func (s *suite) testOperatorCLI(t *testing.T) {
	t.Log("running preview through the CLI and checking idempotency")
	createArguments := []string{
		"run", "create",
		"--target", s.targetID.String(),
		"--action", "preview",
		"--set", "message=hello-kind",
		"--idempotency-key", "acceptance-preview",
		"--format", "json",
	}
	var preview api.Run
	s.mustProductCLIJSON(t, &preview, createArguments...)
	s.previewRunID = preview.ID
	var previewRetry api.Run
	s.mustProductCLIJSON(t, &previewRetry, createArguments...)
	if previewRetry.ID != s.previewRunID {
		t.Fatalf("idempotent preview retry created Run %s, want %s", previewRetry.ID, s.previewRunID)
	}

	var previewWait api.Run
	s.mustProductCLIJSON(t, &previewWait,
		"run", "wait", s.previewRunID.String(),
		"--timeout", s.timeout.String(),
		"--poll-interval", "250ms",
		"--format", "json")
	if previewWait.ID != s.previewRunID || previewWait.Phase != api.RunPhaseFinished ||
		previewWait.Outcome == nil || *previewWait.Outcome != api.RunOutcomeSucceeded {
		t.Fatalf("preview wait response = %#v", previewWait)
	}

	var previewRead api.Run
	s.mustProductCLIJSON(t, &previewRead, "run", "get", s.previewRunID.String(), "--format", "json")
	message, err := previewRead.Parameters["message"].AsParameterValue0()
	if err != nil {
		t.Fatalf("read preview message parameter: %v", err)
	}
	if previewRead.Commit.Sha != s.fixtureCommit || message != "hello-kind" {
		t.Fatalf("preview Run commit/message = %s/%q, want %s/hello-kind", previewRead.Commit.Sha, message, s.fixtureCommit)
	}

	var previewArtifacts api.ArtifactList
	s.mustProductCLIJSON(t, &previewArtifacts, "artifact", "list", s.previewRunID.String(), "--format", "json")
	plan := findArtifact(t, previewArtifacts.Items, "plan")
	planPath := filepath.Join(s.temporaryDirectory, "plan.txt")
	s.mustProductCLI(t, "artifact", "download", plan.ID.String(), "--output", planPath)
	if contents := string(readFile(t, planPath)); contents != "plan:hello-kind\n" && contents != "plan:hello-kind" {
		t.Fatalf("downloaded plan = %q, want plan:hello-kind", contents)
	}
	logContents := s.mustProductCLI(t, "run", "logs", s.previewRunID.String())
	if !bytes.Contains(logContents, []byte("acceptance preview complete: hello-kind")) {
		t.Fatalf("CLI preview log omitted completion message:\n%s", logContents)
	}

	t.Log("applying the exact preview artifact through the CLI")
	var apply api.Run
	s.mustProductCLIJSON(t, &apply,
		"run", "create",
		"--target", s.targetID.String(),
		"--action", "apply",
		"--input", "plan="+plan.ID.String(),
		"--idempotency-key", "acceptance-apply",
		"--format", "json")
	s.applyRunID = apply.ID
	s.waitForRunOutcome(t, s.applyRunID, api.RunOutcomeSucceeded)
	s.mustProductCLIJSON(t, &apply, "run", "get", s.applyRunID.String(), "--format", "json")
	input, ok := apply.Inputs["plan"]
	if !ok || apply.Commit.Sha != previewRead.Commit.Sha ||
		apply.CatalogRevisionID != previewRead.CatalogRevisionID || input.ArtifactID != plan.ID {
		t.Fatalf("apply Run did not retain preview provenance: %#v", apply)
	}
	s.assertLogContains(t, s.applyRunID, "acceptance apply consumed: plan:hello-kind")

	var runs api.RunList
	s.mustProductCLIJSON(t, &runs,
		"run", "list", "--target-id", s.targetID.String(), "--all", "--limit", "1", "--format", "json")
	if runs.Limit != 1 || runs.Offset != 0 || runs.Total < 2 ||
		!containsRun(runs.Items, s.previewRunID) || !containsRun(runs.Items, s.applyRunID) {
		t.Fatalf("CLI Run list did not aggregate preview and apply Runs: %#v", runs)
	}

	t.Log("rerunning the exact preview through the CLI")
	var rerun api.Run
	s.mustProductCLIJSON(t, &rerun,
		"run", "rerun", s.previewRunID.String(),
		"--mode", "exact",
		"--idempotency-key", "acceptance-cli-rerun",
		"--format", "json")
	s.rerunID = rerun.ID
	s.waitForRunOutcome(t, s.rerunID, api.RunOutcomeSucceeded)
	s.assertLogContains(t, s.rerunID, "acceptance preview complete: hello-kind")
}

func (s *suite) testCancellation(t *testing.T) {
	t.Log("testing queued and running cancellation")
	cancelRun := s.createRun(t, "cancel", nil, nil, "acceptance-running-cancel")
	s.canceledRunID = cancelRun.ID
	s.waitForRunPhase(t, s.canceledRunID, api.RunPhaseRunning)
	queuedRun := s.createRun(t, "preview", map[string]api.ParameterValue{
		"message": stringParameter(t, "hello-kind"),
	}, nil, "acceptance-queued-cancel")
	s.waitForRunPhase(t, queuedRun.ID, api.RunPhaseQueued)

	s.cancelRun(t, queuedRun.ID)
	s.waitForRunOutcome(t, queuedRun.ID, api.RunOutcomeCanceled)
	s.assertCLIWaitOutcome(t, queuedRun.ID, api.RunOutcomeCanceled)

	runningCancel := s.cancelRun(t, s.canceledRunID)
	if runningCancel.CancellationRequestedAt == nil {
		t.Fatalf("running cancellation response = %#v", runningCancel)
	}
	s.waitForRunOutcome(t, s.canceledRunID, api.RunOutcomeCanceled)
	s.assertLogContains(t, s.canceledRunID, "acceptance cancellation observed")
	s.waitForJobDeletion(t, s.canceledRunID)
}

func (s *suite) testControllerRestartRecovery(t *testing.T) {
	t.Log("testing Controller restart recovery")
	run := s.createRun(t, "recover", nil, nil, "acceptance-controller-recovery")
	s.controllerRecoveryRun = run.ID
	s.waitForRunPhase(t, run.ID, api.RunPhaseRunning)
	s.restartDeployment(t, "slipramp-controller")
	s.waitForRunOutcome(t, run.ID, api.RunOutcomeSucceeded)
	s.assertLogContains(t, run.ID, "acceptance recovery completed")
}

func (s *suite) testServerRestartRecovery(t *testing.T) {
	t.Log("testing Server restart recovery")
	run := s.createRun(t, "recover", nil, nil, "acceptance-server-recovery")
	s.serverRecoveryRun = run.ID
	s.waitForRunPhase(t, run.ID, api.RunPhaseRunning)
	s.restartDeployment(t, "slipramp-server")
	s.startForward(t, "server", "slipramp-server", s.serverPort, 8080)
	s.waitForRunOutcome(t, run.ID, api.RunOutcomeSucceeded)
	s.assertLogContains(t, run.ID, "acceptance recovery completed")
}

func (s *suite) testRustFSOutageRecovery(t *testing.T) {
	t.Log("testing durable log recovery across a RustFS outage")
	run := s.createRun(t, "recover", nil, nil, "acceptance-rustfs-recovery")
	s.rustFSRecoveryRun = run.ID
	s.waitForRunPhase(t, run.ID, api.RunPhaseRunning)

	firstLine := []byte("acceptance recovery started\n")
	s.waitForDurableLogBytes(t, run.ID, int64(len(firstLine)))

	outageActive := true
	defer func() {
		if outageActive {
			s.setRustFSAvailable(t, true)
		}
	}()
	s.setRustFSAvailable(t, false)
	report := s.waitForReadiness(t, api.Available, api.Unavailable)
	if report.Status != api.NotReady {
		t.Fatalf("readiness during RustFS outage = %#v", report)
	}

	s.kubectl(t,
		"-n", runNamespace,
		"wait", "--for=condition=complete", "job",
		"-l", "slipramp.com/run-id="+run.ID.String(),
		"--timeout="+s.timeout.String(),
	)
	duringOutage := s.getRun(t, run.ID)
	if duringOutage.Phase == api.RunPhaseFinished {
		t.Fatalf("Run %s finished before its log could be retained after the RustFS outage", run.ID)
	}

	s.setRustFSAvailable(t, true)
	outageActive = false
	report = s.waitForReadiness(t, api.Available, api.Available)
	if report.Status != api.Ready {
		t.Fatalf("readiness after RustFS recovery = %#v", report)
	}
	s.waitForRunOutcome(t, run.ID, api.RunOutcomeSucceeded)

	expectedLog := []byte("acceptance recovery started\nacceptance recovery completed\n")
	s.assertSealedLogEquals(t, run.ID, expectedLog)
}

func (s *suite) testCancellationDuringRustFSOutage(t *testing.T) {
	t.Log("testing cancellation delivery while durable log storage is unavailable")
	run := s.createRun(t, "cancel", nil, nil, "acceptance-rustfs-cancellation")
	s.rustFSCancellationRun = run.ID
	s.waitForRunPhase(t, run.ID, api.RunPhaseRunning)

	firstLine := []byte("acceptance cancellation started\n")
	s.waitForDurableLogBytes(t, run.ID, int64(len(firstLine)))

	outageActive := true
	defer func() {
		if outageActive {
			s.setRustFSAvailable(t, true)
		}
	}()
	s.setRustFSAvailable(t, false)
	report := s.waitForReadiness(t, api.Available, api.Unavailable)
	if report.Status != api.NotReady {
		t.Fatalf("readiness during RustFS outage = %#v", report)
	}

	firstCancel := s.cancelRun(t, run.ID)
	secondCancel := s.cancelRun(t, run.ID)
	if firstCancel.CancellationRequestedAt == nil || secondCancel.CancellationRequestedAt == nil ||
		!firstCancel.CancellationRequestedAt.Equal(*secondCancel.CancellationRequestedAt) {
		t.Fatalf("repeated cancellation changed the recorded request: first=%#v second=%#v", firstCancel, secondCancel)
	}
	s.waitForRunCancellationRequest(t, run.ID)
	s.kubectl(t,
		"-n", runNamespace,
		"wait", "--for=condition=complete", "job",
		"-l", "slipramp.com/run-id="+run.ID.String(),
		"--timeout="+s.timeout.String(),
	)
	duringOutage := s.getRun(t, run.ID)
	if duringOutage.Phase == api.RunPhaseFinished {
		t.Fatalf("Run %s finished before its canceled log could be retained", run.ID)
	}

	s.setRustFSAvailable(t, true)
	outageActive = false
	report = s.waitForReadiness(t, api.Available, api.Available)
	if report.Status != api.Ready {
		t.Fatalf("readiness after RustFS recovery = %#v", report)
	}
	finished := s.waitForRunOutcome(t, run.ID, api.RunOutcomeCanceled)
	if finished.OutcomeReason == nil || *finished.OutcomeReason != "CancelRequested" {
		t.Fatalf("canceled Run outcome = %#v", finished)
	}

	expectedLog := []byte("acceptance cancellation started\nacceptance cancellation observed\n")
	s.assertSealedLogEquals(t, run.ID, expectedLog)
	s.waitForJobDeletion(t, run.ID)
}

func (s *suite) testPostgresOutageRecovery(t *testing.T) {
	t.Log("testing active and queued Run recovery across a PostgreSQL outage")
	active := s.createRun(t, "recover", nil, nil, "acceptance-postgres-recovery")
	s.postgresRecoveryRun = active.ID
	s.waitForRunPhase(t, active.ID, api.RunPhaseRunning)

	firstLine := []byte("acceptance recovery started\n")
	s.waitForDurableLogBytes(t, active.ID, int64(len(firstLine)))
	queued := s.createRun(t, "recover", nil, nil, "acceptance-postgres-queued")
	s.postgresQueuedRun = queued.ID
	s.waitForRunPhase(t, queued.ID, api.RunPhaseQueued)

	outageActive := true
	defer func() {
		if outageActive {
			s.setPostgresAvailable(t, true)
		}
	}()
	s.setPostgresAvailable(t, false)
	report := s.waitForReadiness(t, api.Unavailable, api.Available)
	if report.Status != api.NotReady {
		t.Fatalf("readiness during PostgreSQL outage = %#v", report)
	}
	s.assertHealthy(t)

	s.kubectl(t,
		"-n", runNamespace,
		"wait", "--for=condition=complete", "job",
		"-l", "slipramp.com/run-id="+active.ID.String(),
		"--timeout="+s.timeout.String(),
	)
	s.assertJobExists(t, active.ID)
	s.assertJobAbsent(t, queued.ID)

	s.setPostgresAvailable(t, true)
	outageActive = false
	report = s.waitForReadiness(t, api.Available, api.Available)
	if report.Status != api.Ready {
		t.Fatalf("readiness after PostgreSQL recovery = %#v", report)
	}

	expectedLog := []byte("acceptance recovery started\nacceptance recovery completed\n")
	s.waitForRunOutcome(t, active.ID, api.RunOutcomeSucceeded)
	s.assertSealedLogEquals(t, active.ID, expectedLog)
	s.waitForJobDeletion(t, active.ID)

	s.waitForRunOutcome(t, queued.ID, api.RunOutcomeSucceeded)
	s.assertSealedLogEquals(t, queued.ID, expectedLog)
	s.waitForJobDeletion(t, queued.ID)
}

func (s *suite) testRunReviews(t *testing.T) {
	t.Log("testing approval and rejection of review-required Git Runs")
	principalID := s.createServiceAccount(t, "Run review acceptance", "operator")
	reviewAPIKey := s.createServiceAccountAPIKey(t, principalID, "Run review decisions",
		[]string{"slipramp:read", "slipramp:runs:review"})
	runWriteAPIKey := s.createServiceAccountAPIKey(t, principalID, "Run write without review",
		[]string{"slipramp:read", "slipramp:runs:write"})

	s.updateRepositoryHead(t, s.approvedReviewCommit)
	s.waitForRepository(t, s.approvedReviewCommit)
	approved := s.waitForPendingReviewRun(t, s.approvedReviewCommit)
	s.approvedReviewRun = approved.ID
	s.assertPendingReviewNotDispatched(t, approved.ID)

	var pendingRuns api.RunList
	s.mustCLIJSON(t, reviewAPIKey, &pendingRuns,
		"run", "list", "--review-state", "pending", "--all", "--format", "json")
	if !slices.ContainsFunc(pendingRuns.Items, func(run api.Run) bool {
		return run.ID == approved.ID && run.Review.State == api.RunReviewStatePending
	}) {
		t.Fatalf("CLI pending-review Runs omitted %s: %#v", approved.ID, pendingRuns)
	}
	assertInboxHasRunReview(t, s.getInbox(t, reviewAPIKey), approved.ID, true)
	assertInboxHasRunReview(t, s.getInbox(t, runWriteAPIKey), approved.ID, false)

	approvalComment := "Approved by the review-only acceptance key."
	approvalBody := api.RunReviewDecisionRequest{
		Decision: api.RunReviewDecisionApproved,
		Comment:  &approvalComment,
	}
	s.assertPostForbidden(t, reviewAPIKey, "/api/v1/runs", api.RunCreate{
		TargetID: s.targetID,
		Action:   "preview",
		Parameters: &map[string]api.ParameterValue{
			"message": stringParameter(t, "hello-kind"),
		},
	})
	s.assertPostForbidden(t, reviewAPIKey, "/api/v1/runs/"+approved.ID.String()+"/cancel", nil)
	s.assertPostForbidden(t, runWriteAPIKey, "/api/v1/runs/"+approved.ID.String()+"/review", approvalBody)

	var approvedResponse api.Run
	s.mustCLIJSON(t, reviewAPIKey, &approvedResponse,
		"run", "review", "approve", approved.ID.String(),
		"--comment", approvalComment, "--format", "json")
	if approvedResponse.Commit.Sha != s.approvedReviewCommit || approvedResponse.Source.Type != api.RunSourceTypeGit ||
		approvedResponse.Review.State != api.RunReviewStateApproved || approvedResponse.Review.Comment == nil ||
		*approvedResponse.Review.Comment != approvalComment || approvedResponse.Review.DecidedBy == nil ||
		approvedResponse.Review.DecidedBy.DisplayName != "Run review acceptance" {
		t.Fatalf("approved review response = %#v", approvedResponse)
	}
	s.waitForRunOutcome(t, approved.ID, api.RunOutcomeSucceeded)
	s.assertLogContains(t, approved.ID, "acceptance reviewed Git run: approved")
	approvedRead := s.getRun(t, approved.ID)
	if approvedRead.Review.Comment == nil || *approvedRead.Review.Comment != approvalComment {
		t.Fatalf("approved Run comment = %#v", approvedRead.Review.Comment)
	}
	page := s.httpRequest(t, s.sessionHTTPClient, http.MethodGet, s.webURL+"/runs/"+approved.ID.String(), "", nil, nil)
	if page.status != http.StatusOK || !bytes.Contains(page.body, []byte(approvalComment)) {
		t.Fatalf("approved Run web page omitted comment (HTTP %d)", page.status)
	}

	s.updateRepositoryHead(t, s.rejectedReviewCommit)
	s.waitForRepository(t, s.rejectedReviewCommit)
	rejected := s.waitForPendingReviewRun(t, s.rejectedReviewCommit)
	s.rejectedReviewRun = rejected.ID
	s.assertPendingReviewNotDispatched(t, rejected.ID)
	rejectionComment := "Rejected by the review-only acceptance key."
	var rejectedResponse api.Run
	s.mustCLIJSON(t, reviewAPIKey, &rejectedResponse,
		"run", "review", "reject", rejected.ID.String(),
		"--comment", rejectionComment, "--format", "json")
	if rejectedResponse.Commit.Sha != s.rejectedReviewCommit || rejectedResponse.Phase != api.RunPhaseFinished ||
		rejectedResponse.Outcome == nil || *rejectedResponse.Outcome != api.RunOutcomeCanceled ||
		rejectedResponse.OutcomeReason == nil || *rejectedResponse.OutcomeReason != "ReviewRejected" ||
		rejectedResponse.Review.State != api.RunReviewStateRejected || rejectedResponse.Review.Comment == nil ||
		*rejectedResponse.Review.Comment != rejectionComment || rejectedResponse.Review.DecidedBy == nil ||
		rejectedResponse.Review.DecidedBy.DisplayName != "Run review acceptance" {
		t.Fatalf("rejected review response = %#v", rejectedResponse)
	}
	s.waitForRunOutcome(t, rejected.ID, api.RunOutcomeCanceled)
	s.waitForJobDeletion(t, rejected.ID)
	inbox := s.getInbox(t, reviewAPIKey)
	for _, decision := range inbox.Decisions {
		if decision.Kind == api.InboxItemKindRunReview &&
			(decision.Destination.ID == approved.ID.String() || decision.Destination.ID == rejected.ID.String()) {
			t.Fatalf("decided Run review remained in Inbox: %#v", decision)
		}
	}
}

func (s *suite) testInbox(t *testing.T) {
	t.Log("testing operational Inbox projection and personal acknowledgements")
	principalID := s.createServiceAccount(t, "Inbox acceptance", "operator")
	inboxAPIKey := s.createServiceAccountAPIKey(t, principalID, "Inbox acceptance",
		[]string{"slipramp:read", "slipramp:inbox:write"})

	s.scaleDeployment(t, "slipramp-controller", 0)
	firstProblem := s.waitForClusterInboxProblem(t, s.productAPIKey)
	secondPrincipalProblem := s.waitForClusterInboxProblem(t, inboxAPIKey)
	if firstProblem != secondPrincipalProblem {
		t.Fatalf("Inbox problem identities differ by principal: %s != %s", firstProblem, secondPrincipalProblem)
	}

	page := s.httpRequest(t, s.sessionHTTPClient, http.MethodGet, s.webURL+"/", "", nil, nil)
	if page.status != http.StatusOK || !bytes.Contains(page.body, []byte("kind is unreachable")) ||
		!bytes.Contains(page.body, []byte("Acknowledge all")) {
		t.Fatalf("Inbox web page omitted unreachable problem or acknowledgement action (HTTP %d)", page.status)
	}
	form := url.Values{"problemId": []string{firstProblem}}
	headers := make(http.Header)
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	headers.Set("Origin", s.webURL)
	acknowledge := s.httpRequest(t, s.sessionHTTPClient, http.MethodPost, s.webURL+"/?index", "", []byte(form.Encode()), headers)
	if acknowledge.status < 200 || acknowledge.status >= 400 {
		t.Fatalf("acknowledge Inbox problem returned HTTP %d: %s", acknowledge.status, acknowledge.body)
	}
	productInbox := s.getInbox(t, s.productAPIKey)
	if containsInboxItem(productInbox.Problems, firstProblem) || len(productInbox.Decisions) != 2 {
		t.Fatalf("acknowledged product Inbox = %#v", productInbox)
	}
	otherInbox := s.getInbox(t, inboxAPIKey)
	if !containsInboxItem(otherInbox.Problems, firstProblem) {
		t.Fatalf("acknowledgement leaked to second principal: %#v", otherInbox)
	}

	s.restartDeployment(t, "slipramp-web")
	s.startForward(t, "web", "slipramp-web", s.webPort, 3000)
	page = s.httpRequest(t, s.sessionHTTPClient, http.MethodGet, s.webURL+"/", "", nil, nil)
	if bytes.Contains(page.body, []byte("kind is unreachable")) || !bytes.Contains(page.body, []byte("Needs a decision")) {
		t.Fatalf("Inbox web page did not preserve acknowledgement and decisions after restart")
	}

	s.scaleDeployment(t, "slipramp-controller", 1)
	s.waitForDeployment(t, "slipramp-controller")
	s.waitForClusterInboxRecovery(t, inboxAPIKey)
	s.scaleDeployment(t, "slipramp-controller", 0)
	recurrentProblem := s.waitForClusterInboxProblem(t, s.productAPIKey)
	if recurrentProblem == firstProblem {
		t.Fatalf("recurrent Inbox problem reused occurrence identity %s", firstProblem)
	}
	s.scaleDeployment(t, "slipramp-controller", 1)
	s.waitForDeployment(t, "slipramp-controller")
}

func findArtifact(t *testing.T, artifacts []api.Artifact, name string) api.Artifact {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.Name == name {
			return artifact
		}
	}
	t.Fatalf("Artifact list omitted %q: %#v", name, artifacts)
	return api.Artifact{}
}

func containsRun(runs []api.Run, id uuid.UUID) bool {
	return slices.ContainsFunc(runs, func(run api.Run) bool { return run.ID == id })
}

func (s *suite) restartDeployment(t *testing.T, name string) {
	t.Helper()
	s.kubectl(t, "-n", systemNamespace, "rollout", "restart", "deployment/"+name)
	s.waitForDeployment(t, name)
}

func (s *suite) scaleDeployment(t *testing.T, name string, replicas int) {
	t.Helper()
	s.kubectl(t, "-n", systemNamespace, "scale", "deployment/"+name, "--replicas="+strconv.Itoa(replicas))
}

func (s *suite) waitForDeployment(t *testing.T, name string) {
	t.Helper()
	s.kubectl(t, "-n", systemNamespace, "rollout", "status", "deployment/"+name, "--timeout="+s.timeout.String())
}

func (s *suite) setRustFSAvailable(t *testing.T, available bool) {
	t.Helper()

	signal := "STOP"
	if available {
		signal = "CONT"
	}
	script := `read -r rustfs_pid < /tmp/rustfs.pid; kill -` + signal + ` "$rustfs_pid"`
	s.kubectl(t,
		"-n", systemNamespace,
		"exec", "deployment/rustfs",
		"--", "/bin/sh", "-c", script,
	)
}

func (s *suite) setPostgresAvailable(t *testing.T, available bool) {
	t.Helper()

	script := `pidof postgres > /tmp/postgres.pids; ` +
		`for postgres_pid in $(cat /tmp/postgres.pids); do kill -STOP "$postgres_pid"; done`
	if available {
		script = `for postgres_pid in $(cat /tmp/postgres.pids); do kill -CONT "$postgres_pid"; done`
	}
	s.kubectl(t,
		"-n", systemNamespace,
		"exec", "deployment/postgres",
		"--", "/bin/sh", "-c", script,
	)
}

func (s *suite) getInbox(t *testing.T, key string) api.Inbox {
	t.Helper()
	client := s.newAPIClient(t, key)
	response, err := client.GetInboxWithResponse(t.Context())
	if err != nil {
		t.Fatalf("get Inbox: %v", err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil {
		t.Fatal("Inbox response is empty")
	}
	return *response.JSON200
}

func assertInboxHasRunReview(t *testing.T, inbox api.Inbox, runID uuid.UUID, expected bool) {
	t.Helper()
	found := slices.ContainsFunc(inbox.Decisions, func(item api.InboxItem) bool {
		return item.Kind == api.InboxItemKindRunReview && item.Destination.Type == "run" &&
			item.Destination.ID == runID.String()
	})
	if found != expected {
		t.Fatalf("Inbox Run review %s presence = %t, want %t: %#v", runID, found, expected, inbox.Decisions)
	}
}

func (s *suite) waitForClusterInboxProblem(t *testing.T, key string) string {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var lastInbox api.Inbox
	for time.Now().Before(deadline) {
		lastInbox = s.getInbox(t, key)
		for _, problem := range lastInbox.Problems {
			if problem.Kind == api.InboxItemKindClusterUnreachable && problem.Destination.ID == "kind" {
				return problem.ID
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("cluster-unreachable Inbox problem did not appear: %#v", lastInbox)
	return ""
}

func (s *suite) waitForClusterInboxRecovery(t *testing.T, key string) {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var lastInbox api.Inbox
	for time.Now().Before(deadline) {
		lastInbox = s.getInbox(t, key)
		unreachable := slices.ContainsFunc(lastInbox.Problems, func(problem api.InboxItem) bool {
			return problem.Kind == api.InboxItemKindClusterUnreachable && problem.Destination.ID == "kind"
		})
		if !unreachable {
			return
		}
		wait(t, time.Second)
	}
	t.Fatalf("cluster-unreachable Inbox problem did not recover: %#v", lastInbox)
}

func containsInboxItem(items []api.InboxItem, id string) bool {
	return slices.ContainsFunc(items, func(item api.InboxItem) bool { return item.ID == id })
}
