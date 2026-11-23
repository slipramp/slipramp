package controller

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controller/jobpolicy"
	"github.com/slipramp/slipramp/internal/controller/runjob"
	"github.com/slipramp/slipramp/internal/run"
)

func TestControllerObservesJobFailureAfterPodEviction(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"continuous", "restart during gap"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				testControllerObservesEviction(t, scenario == "restart during gap")
			})
		})
	}
}

func testControllerObservesEviction(t *testing.T, restart bool) {
	t.Helper()
	ctx := t.Context()
	runID := uuid.New()
	job := &batchv1.Job{
		Name:      "slipramp-run-" + runID.String(),
		Namespace: "runs",
		UID:       "job-uid",
		Labels: map[string]string{
			jobpolicy.ManagedByLabel: jobpolicy.ManagedByLabelValue,
			jobpolicy.ClusterLabel:   "colo",
			"slipramp.com/run-id":    runID.String(),
		},
	}
	pod := &corev1.Pod{
		Name:      "run-pod",
		Namespace: job.Namespace,
		Labels:    maps.Clone(job.Labels),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job")),
		},
	}
	pod.Labels[batchv1.JobNameLabel] = job.Name
	pod.Labels[batchv1.ControllerUidLabel] = string(job.UID)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "action", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
	}}
	kube := fake.NewClientset(job, pod)
	logs := &memoryLogReporter{}
	state := run.ObservationTransitionState{Phase: run.PhaseAssigned}
	var received api.RunObservation
	transport := httpDoerFunc(func(req *http.Request) (*http.Response, error) {
		var report api.RunObservation
		if err := json.NewDecoder(req.Body).Decode(&report); err != nil {
			t.Errorf("decode Run observation: %v", err)
			return nil, err
		}
		if req.URL.Path != "/controller/v1/runs/"+runID.String()+"/observations" ||
			report.JobUID == nil || *report.JobUID != string(job.UID) {
			t.Errorf("unexpected Run or Job identity on report: %s, %#v", req.URL.Path, report)
		}
		observation := run.Observation{
			Phase: run.Phase(report.Phase), Outcome: (*run.Outcome)(report.Outcome),
			JobUID: report.JobUID, Reason: report.Reason,
		}
		response := httptest.NewRecorder()
		if err := run.ValidateObservationTransition(state, observation); err != nil {
			t.Errorf("Server rejected %s → %s: %v", state.Phase, observation.Phase, err)
			response.WriteHeader(http.StatusConflict)
			return response.Result(), nil
		}
		if state.Phase == run.PhaseAssigned && observation.Phase == run.PhaseRunning {
			if err := kube.CoreV1().Pods(job.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
				t.Errorf("evict running Pod: %v", err)
			}
		}
		if report.Phase == api.RunObservationPhaseFinished && !logs.isFailed() {
			t.Error("terminal observation arrived before the partial log was finalized")
		}
		received = report
		state.Phase = observation.Phase
		state.JobUID = observation.JobUID
		response.WriteHeader(http.StatusNoContent)
		return response.Result(), nil
	})
	reports, err := newServerClientWithHTTPClient(validConfig(), transport)
	if err != nil {
		t.Fatal(err)
	}

	recoverRuns := func() (*Controller, context.CancelFunc) {
		t.Helper()
		config := runjob.Config{ClusterName: "colo", ExecutionNamespaces: []string{job.Namespace}}
		app := newTestController(runjob.New(config, kube.BatchV1(), kube.CoreV1()), logs)
		app.reports = reports
		workerCtx, stop := context.WithCancel(ctx)
		if !app.recoverRuns(workerCtx) {
			stop()
			t.Fatal("Controller could not recover execution Jobs")
		}
		synctest.Wait()
		return app, stop
	}
	app, stop := recoverRuns()
	defer stop()
	if state.Phase != run.PhaseRunning || len(logs.bytes()) == 0 {
		t.Fatal("Controller did not report Running and forward the Pod log before eviction")
	}
	prefix := string(logs.bytes())
	if restart {
		stop()
		app.runWorkers.Wait()
		app, stop = recoverRuns()
		defer stop()
	}
	// Let the worker observe the missing Pod while the Job still has no conditions.
	time.Sleep(app.observeEvery)
	synctest.Wait()

	job.Status.Conditions = []batchv1.JobCondition{{
		Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded",
	}}
	jobs := kube.BatchV1().Jobs(job.Namespace)
	if _, err := jobs.UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(app.observeEvery)
	synctest.Wait()
	if received.Phase != api.RunObservationPhaseFinished ||
		received.Outcome == nil || *received.Outcome != api.RunObservationOutcomeFailed ||
		received.Reason == nil || *received.Reason != "BackoffLimitExceeded" {
		t.Fatalf("terminal observation = %#v", received)
	}
	if _, err := jobs.Get(ctx, job.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("get Job after terminal report: %v; want NotFound", err)
	}
	if got := string(logs.bytes()); got != prefix {
		t.Fatalf("retained log = %q, want %q", got, prefix)
	}
}

func TestControllerReportsPreparingForNewJobWithoutPod(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	handle := runjob.Handle{RunID: uuid.New(), Namespace: "runs", UID: "new-job"}
	jobs := &stubRunJobs{
		ensure: func(context.Context, api.RunAssignment) (runjob.Ensured, error) {
			return runjob.Ensured{Handle: handle, Created: true}, nil
		},
		observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
			return runjob.Observation{JobUID: &handle.UID}, nil
		},
	}
	app := newTestController(jobs, &memoryLogReporter{})
	var received api.RunObservation
	app.reports = runObservationReporterFunc(func(_ context.Context, _ uuid.UUID, report api.RunObservation) error {
		received = report
		cancel()
		return nil
	})
	app.startRunWorker(ctx, handle, &api.RunAssignment{ID: handle.RunID}, nil)
	app.runWorkers.Wait()
	if received.Phase != api.RunObservationPhasePreparing || received.JobUID == nil || *received.JobUID != handle.UID {
		t.Fatalf("first observation of newly created Job = %#v", received)
	}
}

func TestControllerStopsObservationOnJobIdentityConflict(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"Kubernetes", "Server"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			handle := runjob.Handle{RunID: uuid.New(), Namespace: "runs", UID: "job-uid"}
			jobs := &stubRunJobs{
				observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
					if source == "Kubernetes" {
						return runjob.Observation{}, runjob.ErrRunJobIdentityMismatch
					}
					return runjob.Observation{Phase: api.RunObservationPhasePreparing, JobUID: &handle.UID}, nil
				},
				delete: func(context.Context, runjob.Handle) error {
					t.Error("deleted Job after identity conflict")
					return nil
				},
			}
			app := newTestController(jobs, &memoryLogReporter{})
			app.reports = runObservationReporterFunc(func(context.Context, uuid.UUID, api.RunObservation) error {
				if source == "Kubernetes" {
					t.Error("reported observation after Kubernetes identity mismatch")
				}
				return newServerResponseError("report Run observation", http.StatusConflict)
			})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			worker := app.startRunWorker(ctx, handle, nil, nil)
			app.runWorkers.Wait()
			app.reapRunWorkers()
			if !worker.blocked || app.activeRuns[handle.RunID] != worker {
				t.Fatal("worker did not stop and retain the conflicted Run")
			}
		})
	}
}

func TestShouldReportRunObservation(t *testing.T) {
	t.Parallel()

	reportedAt := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	reported := runjob.Observation{Phase: api.RunObservationPhasePreparing}
	running := runjob.Observation{Phase: api.RunObservationPhaseRunning}
	heartbeatEvery := 15 * time.Second
	tests := []struct {
		name        string
		observation runjob.Observation
		reported    *runjob.Observation
		now         time.Time
		want        bool
	}{
		{
			name:        "first observation",
			observation: reported,
			now:         reportedAt,
			want:        true,
		},
		{
			name:        "changed observation",
			observation: running,
			reported:    &reported,
			now:         reportedAt,
			want:        true,
		},
		{
			name:        "unchanged before heartbeat",
			observation: reported,
			reported:    &reported,
			now:         reportedAt.Add(heartbeatEvery - time.Nanosecond),
		},
		{
			name:        "unchanged at heartbeat",
			observation: reported,
			reported:    &reported,
			now:         reportedAt.Add(heartbeatEvery),
			want:        true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := shouldReport(
				test.observation,
				test.reported,
				reportedAt,
				test.now,
				heartbeatEvery,
			)
			if got != test.want {
				t.Errorf("shouldReport() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestObservationRequestIncludesExecutionImages(t *testing.T) {
	t.Parallel()

	action := "registry.example.com/tools@sha256:action"
	checkout := "registry.example.com/checkout@sha256:checkout"
	harness := "registry.example.com/harness@sha256:harness"
	request := observationRequest(runjob.Observation{
		Phase: api.RunObservationPhaseRunning,
		Images: api.ExecutionImages{
			Action: &action, Checkout: &checkout, Harness: &harness,
		},
	})
	if request.Images == nil || request.Images.Action == nil || *request.Images.Action != action ||
		request.Images.Checkout == nil || *request.Images.Checkout != checkout ||
		request.Images.Harness == nil || *request.Images.Harness != harness {
		t.Errorf("observation request images = %#v", request.Images)
	}
}
