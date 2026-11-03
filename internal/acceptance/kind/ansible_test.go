package kindacceptance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

func (s *suite) testAnsible(t *testing.T) {
	t.Log("testing Ansible preview and strict managed-host SSH")
	preview := s.createRunForTarget(t, s.ansibleTargetID, "preview", nil, nil, "ansible-preview")
	s.waitForRunOutcome(t, preview.ID, api.RunOutcomeSucceeded)
	previewReport, previewBody := s.ansibleReport(t, preview.ID)
	if previewReport.Mode != api.AnsiblePlaybookReportModePreview ||
		previewReport.Runner.Status != api.AnsibleRunnerSummaryStatusSuccessful ||
		!previewReport.Summary.Complete || previewReport.Summary.Results.Changed < 1 {
		t.Fatalf("Ansible preview report = %#v", previewReport)
	}
	s.assertLogContains(t, preview.ID, "Apply the acceptance baseline")
	s.assertAnsibleSecretAbsent(t, preview.ID, previewBody)

	t.Log("testing Ansible apply, report projection, and no_log redaction")
	apply := s.createRunForTarget(t, s.ansibleTargetID, "apply", nil, nil, "ansible-apply")
	s.waitForRunOutcome(t, apply.ID, api.RunOutcomeSucceeded)
	applyReport, applyBody := s.ansibleReport(t, apply.ID)
	if applyReport.Mode != api.AnsiblePlaybookReportModeApply ||
		applyReport.Runner.Status != api.AnsibleRunnerSummaryStatusSuccessful ||
		!applyReport.Summary.Complete || applyReport.Summary.Results.Changed < 1 {
		t.Fatalf("Ansible apply report = %#v", applyReport)
	}
	redacted := false
	for _, play := range applyReport.Plays {
		for _, task := range play.Tasks {
			if !task.Redacted {
				continue
			}
			redacted = true
			if task.Name != "[redacted]" || task.Action != "[redacted]" || task.SourcePath != nil {
				t.Fatalf("Ansible no_log task was not fully redacted: %#v", task)
			}
		}
	}
	if !redacted {
		t.Fatalf("Ansible apply report omitted the no_log redaction: %#v", applyReport.Plays)
	}
	marker := strings.TrimSpace(string(s.kubectl(t, "-n", systemNamespace,
		"exec", "deployment/acceptance-ssh", "-c", "ssh", "--", "cat", "/tmp/slipramp-ansible-marker")))
	if marker != "managed by Slipramp Ansible acceptance" {
		t.Fatalf("Ansible managed-host marker = %q", marker)
	}
	managedSecret := strings.TrimSpace(string(s.kubectl(t, "-n", systemNamespace,
		"exec", "deployment/acceptance-ssh", "-c", "ssh", "--", "cat", "/tmp/slipramp-ansible-secret")))
	if managedSecret != ansibleAcceptanceSecret {
		t.Fatalf("Ansible managed-host secret was not injected correctly")
	}
	s.assertAnsibleSecretAbsent(t, apply.ID, applyBody)

	t.Log("testing failed and unreachable playbooks with retained reports")
	failed := s.createRunForTarget(t, s.ansibleTargetID, "fail", nil, nil, "ansible-failed")
	s.waitForRunOutcome(t, failed.ID, api.RunOutcomeFailed)
	failedReport, failedBody := s.ansibleReport(t, failed.ID)
	if failedReport.Runner.Status != api.AnsibleRunnerSummaryStatusFailed ||
		!failedReport.Summary.Complete || failedReport.Summary.Results.Failed != 1 {
		t.Fatalf("failed Ansible report = %#v", failedReport)
	}
	attempts := strings.TrimSpace(string(s.kubectl(t, "-n", systemNamespace,
		"exec", "deployment/acceptance-ssh", "-c", "ssh", "--", "sh", "-c",
		"wc -l < /tmp/slipramp-ansible-failure-attempts")))
	if attempts != "1" {
		t.Fatalf("failed Ansible action attempts = %q, want 1", attempts)
	}
	s.assertAnsibleSecretAbsent(t, failed.ID, failedBody)

	unreachable := s.createRunForTarget(
		t, s.ansibleTargetID, "unreachable", nil, nil, "ansible-unreachable",
	)
	s.waitForRunOutcome(t, unreachable.ID, api.RunOutcomeFailed)
	unreachableReport, unreachableBody := s.ansibleReport(t, unreachable.ID)
	if unreachableReport.Runner.Status != api.AnsibleRunnerSummaryStatusFailed ||
		!unreachableReport.Summary.Complete || unreachableReport.Summary.Results.Unreachable != 1 {
		t.Fatalf("unreachable Ansible report = %#v", unreachableReport)
	}
	s.assertAnsibleSecretAbsent(t, unreachable.ID, unreachableBody)

	t.Log("testing cancellation, partial upload, Target serialization, and the direct Ansible path")
	canceled := s.createRunForTarget(t, s.ansibleTargetID, "cancel", nil, nil, "ansible-cancel")
	s.waitForRunPhase(t, canceled.ID, api.RunPhaseRunning)
	s.waitForActionLog(t, canceled.ID, "Wait until Slipramp cancels the Run")
	direct := s.createRunForTarget(t, s.ansibleTargetID, "direct", nil, nil, "ansible-direct")
	s.waitForRunPhase(t, direct.ID, api.RunPhaseQueued)
	s.cancelRun(t, canceled.ID)
	s.waitForRunOutcome(t, canceled.ID, api.RunOutcomeCanceled)
	canceledReport, canceledBody := s.ansibleReport(t, canceled.ID)
	if canceledReport.Runner.Status != api.AnsibleRunnerSummaryStatusCanceled || canceledReport.Summary.Complete {
		t.Fatalf("canceled Ansible report = %#v", canceledReport)
	}
	s.assertAnsibleSecretAbsent(t, canceled.ID, canceledBody)
	s.waitForJobDeletion(t, canceled.ID)

	s.waitForRunOutcome(t, direct.ID, api.RunOutcomeSucceeded)
	s.assertLogContains(t, direct.ID, "Apply the acceptance baseline")
	s.assertNoReportArtifact(t, direct.ID)
	s.assertAnsibleSecretAbsent(t, direct.ID, nil)

	for _, deployment := range []string{"slipramp-server", "slipramp-controller"} {
		logs := s.kubectl(t, "-n", systemNamespace, "logs", "deployment/"+deployment, "--all-containers=true")
		if bytes.Contains(logs, []byte(ansibleAcceptanceSecret)) {
			t.Fatalf("%s structured logs contain the injected Ansible secret", deployment)
		}
	}
}

func (s *suite) ansibleReport(t *testing.T, runID uuid.UUID) (api.AnsiblePlaybookReport, []byte) {
	t.Helper()

	artifact := s.artifact(t, runID, "report")
	if artifact.State != api.ArtifactStateSealed || !artifact.Sensitive ||
		artifact.MediaType != "application/vnd.slipramp.ansible-playbook+json" || artifact.Report == nil {
		t.Fatalf("Run %s Ansible report Artifact = %#v", runID, artifact)
	}
	summary, err := artifact.Report.AsAnsiblePlaybookReportSummary()
	if err != nil {
		t.Fatalf("decode Run %s Ansible report summary: %v", runID, err)
	}
	if summary.RunID != runID || summary.ArtifactID != artifact.ID {
		t.Fatalf("Run %s Ansible report summary = %#v", runID, summary)
	}

	response, err := s.productClient.GetArtifactReportWithResponse(t.Context(), artifact.ID)
	if err != nil {
		t.Fatalf("get Run %s Ansible report: %v", runID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil {
		t.Fatalf("Run %s Ansible report response is empty", runID)
	}
	report, err := response.JSON200.AsAnsiblePlaybookReport()
	if err != nil {
		t.Fatalf("decode Run %s Ansible report: %v", runID, err)
	}
	if report.RunID != runID || report.ArtifactID != artifact.ID || report.Tool != "ansible" {
		t.Fatalf("Run %s Ansible report = %#v", runID, report)
	}

	return report, response.Body
}

func (s *suite) assertNoReportArtifact(t *testing.T, runID uuid.UUID) {
	t.Helper()

	response, err := s.productClient.ListRunArtifactsWithResponse(t.Context(), runID)
	if err != nil {
		t.Fatalf("list direct Ansible Run %s artifacts: %v", runID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil {
		t.Fatalf("direct Ansible Run %s returned no Artifact list", runID)
	}
	if slices.ContainsFunc(response.JSON200.Items, func(artifact api.Artifact) bool {
		return artifact.Report != nil || artifact.Name == "report"
	}) {
		t.Fatalf("direct Ansible Run %s unexpectedly produced a report: %#v", runID, response.JSON200.Items)
	}
}

func (s *suite) assertAnsibleSecretAbsent(t *testing.T, runID uuid.UUID, reportBody []byte) {
	t.Helper()

	runBody, err := json.Marshal(s.getRun(t, runID))
	if err != nil {
		t.Fatalf("encode Run %s for secret check: %v", runID, err)
	}
	logArtifact := s.artifact(t, runID, "log")
	logBody := s.downloadArtifact(t, logArtifact.ID)
	for subject, contents := range map[string][]byte{
		"Run API":           runBody,
		"report projection": reportBody,
		"Run log":           logBody,
	} {
		if bytes.Contains(contents, []byte(ansibleAcceptanceSecret)) {
			t.Fatalf("Run %s %s contains the injected Ansible secret", runID, subject)
		}
	}
}

func (s *suite) waitForActionLog(t *testing.T, runID uuid.UUID, pattern string) {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	for time.Now().Before(deadline) {
		result := s.kubectlResult(t,
			"-n", runNamespace,
			"logs", "-l", "slipramp.com/run-id="+runID.String(),
			"-c", "action", "--tail=-1",
		)
		if result.exitCode == 0 && bytes.Contains(result.stdout, []byte(pattern)) {
			return
		}
		wait(t, 250*time.Millisecond)
	}
	t.Fatalf("Run %s action log did not contain %q", runID, pattern)
}
