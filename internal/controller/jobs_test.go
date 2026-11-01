package controller

import (
	"context"
	"errors"
	"io"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/slipramp/slipramp/internal/api"
)

func TestKubernetesRunJobsCreatesAndReusesJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	client.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		created := action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		created.UID = types.UID("job-uid")

		return false, nil, nil
	})
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())
	assignment := testRunAssignment(t)

	ensured, err := jobs.Ensure(context.Background(), assignment)
	if err != nil {
		t.Fatalf("first Ensure() error = %v", err)
	}
	if !ensured.created {
		t.Error("first Ensure() created = false, want true")
	}

	ensured, err = jobs.Ensure(context.Background(), assignment)
	if err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
	if ensured.created {
		t.Error("second Ensure() created = true, want false")
	}

	jobList, err := client.BatchV1().Jobs("slipramp-runs").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobList.Items) != 1 {
		t.Errorf("Jobs = %d, want 1", len(jobList.Items))
	}
}

func TestKubernetesRunJobsMarksPodForCancellationIdempotently(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job, err := renderJob(validConfig(), assignment)
	if err != nil {
		t.Fatalf("renderJob() error = %v", err)
	}
	job.UID = types.UID("job-uid")
	pod := testExecutionPod(job)
	pod.Annotations = map[string]string{cancellationAnnotation: "false"}
	client := fake.NewClientset(job, pod)
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())
	command := api.CancelRunCommand{
		ID:           assignment.ID,
		JobNamespace: assignment.Placement.JobNamespace,
		JobUID:       new("job-uid"),
	}

	result, err := jobs.RequestCancellation(context.Background(), command)
	if err != nil {
		t.Fatalf("first RequestCancellation() error = %v", err)
	}
	if result.handle.uid != "job-uid" || !result.jobRetained {
		t.Errorf("first RequestCancellation() result = %#v", result)
	}
	if _, err := client.BatchV1().Jobs(job.Namespace).Get(
		context.Background(),
		job.Name,
		metav1.GetOptions{},
	); err != nil {
		t.Fatalf("get retained Job: %v", err)
	}
	gotPod, err := client.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get marked Pod: %v", err)
	}
	if gotPod.Annotations[cancellationAnnotation] != "true" {
		t.Errorf("cancellation annotation = %#v", gotPod.Annotations)
	}

	result, err = jobs.RequestCancellation(context.Background(), command)
	if err != nil {
		t.Fatalf("repeated RequestCancellation() error = %v", err)
	}
	if result.handle.uid != "job-uid" || !result.jobRetained {
		t.Errorf("repeated RequestCancellation() result = %#v", result)
	}
}

func TestKubernetesRunJobsDeletesJobWithoutPodOnCancellation(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job, err := renderJob(validConfig(), assignment)
	if err != nil {
		t.Fatalf("renderJob() error = %v", err)
	}
	job.UID = types.UID("job-uid")
	client := fake.NewClientset(job)
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())
	command := api.CancelRunCommand{
		ID:           assignment.ID,
		JobNamespace: assignment.Placement.JobNamespace,
		JobUID:       new("job-uid"),
	}

	result, err := jobs.RequestCancellation(context.Background(), command)
	if err != nil {
		t.Fatalf("RequestCancellation() error = %v", err)
	}
	if result.jobRetained {
		t.Fatalf("RequestCancellation() result = %#v", result)
	}
	_, err = client.BatchV1().Jobs(job.Namespace).Get(context.Background(), job.Name, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("get canceled Job error = %v, want not found", err)
	}
}

func TestKubernetesRunJobsDoesNotRelabelCompletedPodAsCanceled(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job, err := renderJob(validConfig(), assignment)
	if err != nil {
		t.Fatalf("renderJob() error = %v", err)
	}
	job.UID = types.UID("job-uid")
	pod := testExecutionPod(job)
	pod.Status = corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{
			Name: actionContainerName,
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
			},
		},
	}}
	pod.Annotations = map[string]string{cancellationAnnotation: "false"}
	client := fake.NewClientset(job, pod)
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())

	result, err := jobs.RequestCancellation(
		context.Background(),
		api.CancelRunCommand{
			ID:           assignment.ID,
			JobNamespace: assignment.Placement.JobNamespace,
			JobUID:       new("job-uid"),
		},
	)
	if err != nil {
		t.Fatalf("RequestCancellation() error = %v", err)
	}
	if !result.jobRetained {
		t.Fatalf("RequestCancellation() result = %#v", result)
	}
	got, err := client.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get completed Pod: %v", err)
	}
	if got.Annotations[cancellationAnnotation] != "false" {
		t.Errorf("cancellation annotation = %#v", got.Annotations)
	}
}

func TestKubernetesRunJobsDeleteUsesUIDPrecondition(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job := testExecutionJob(t, assignment)
	replacement := job.DeepCopy()
	replacement.UID = types.UID("replacement-uid")
	client := fake.NewClientset(job)
	var deleteOptions metav1.DeleteOptions
	client.PrependReactor(
		"delete",
		"jobs",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			deleteAction, ok := action.(k8stesting.DeleteAction)
			if !ok {
				t.Fatalf("delete action = %T", action)
			}
			deleteOptions = deleteAction.GetDeleteOptions()
			if err := client.Tracker().Update(batchv1.SchemeGroupVersion.WithResource("jobs"), replacement,
				replacement.Namespace); err != nil {
				t.Fatalf("replace Job: %v", err)
			}

			return true, nil, apierrors.NewConflict(
				schema.GroupResource{Group: "batch", Resource: "jobs"},
				replacement.Name,
				errors.New("UID precondition failed"),
			)
		},
	)
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())
	handle := jobHandle{
		runID:     assignment.ID,
		namespace: replacement.Namespace,
		name:      replacement.Name,
		uid:       string(job.UID),
	}

	err := jobs.Delete(context.Background(), handle)
	if !apierrors.IsConflict(err) {
		t.Fatalf("Delete() error = %v, want conflict", err)
	}
	if deleteOptions.Preconditions == nil ||
		deleteOptions.Preconditions.UID == nil ||
		*deleteOptions.Preconditions.UID != types.UID(handle.uid) {
		t.Fatalf("delete preconditions = %#v", deleteOptions.Preconditions)
	}
	got, err := client.BatchV1().Jobs(replacement.Namespace).Get(
		context.Background(),
		replacement.Name,
		metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("get replacement Job: %v", err)
	}
	if got.UID != replacement.UID {
		t.Errorf("replacement Job UID = %q, want %q", got.UID, replacement.UID)
	}
}

func TestKubernetesRunJobsDeletesCompletedJobIdempotently(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job := testExecutionJob(t, assignment)
	client := fake.NewClientset(job)
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())
	handle := handleFromJob(job, assignment.ID)

	if err := jobs.Delete(context.Background(), handle); err != nil {
		t.Fatalf("first Delete() error = %v", err)
	}
	_, err := client.BatchV1().Jobs(job.Namespace).Get(context.Background(), job.Name, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("get deleted Job error = %v, want not found", err)
	}
	if err := jobs.Delete(context.Background(), handle); err != nil {
		t.Fatalf("repeated Delete() error = %v", err)
	}
}

func TestKubernetesRunJobsRejectsCancellationOfConflictingJob(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	job := &batchv1.Job{}
	job.Name = runJobName(runID)
	job.Namespace = "slipramp-runs"
	client := fake.NewClientset(job)
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())

	_, err := jobs.RequestCancellation(context.Background(), api.CancelRunCommand{
		ID:           runID,
		JobNamespace: "slipramp-runs",
	})
	if err == nil || !strings.Contains(err.Error(), "expected Run identity") {
		t.Fatalf("RequestCancellation() error = %v", err)
	}
}

func TestKubernetesRunJobsObservesJobState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		missing            bool
		status             batchv1.JobStatus
		statusAfterPodExit *batchv1.JobStatus
		podStatus          *corev1.ContainerStatus
		canceled           bool
		handleUID          string
		wantPhase          api.RunObservationPhase
		wantOutcome        *api.RunObservationOutcome
		wantReason         string
		wantExit           *int32
		wantImages         api.ExecutionImages
	}{
		{
			name:      "active Job with pending action",
			status:    batchv1.JobStatus{Active: 1},
			wantPhase: api.RunObservationPhasePreparing,
		},
		{
			name: "running",
			podStatus: &corev1.ContainerStatus{
				Name:    actionContainerName,
				ImageID: "registry.example.com/terraform@sha256:action",
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{},
				},
			},
			wantPhase: api.RunObservationPhaseRunning,
			wantImages: api.ExecutionImages{
				Action:   new("registry.example.com/terraform@sha256:action"),
				Checkout: new("registry.example.com/slipramp-checkout@sha256:checkout"),
				Harness:  new("registry.example.com/slipramp-harness@sha256:harness"),
			},
		},
		{
			name: "succeeded",
			status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobComplete,
				Status: corev1.ConditionTrue,
			}}},
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeSucceeded),
			wantExit:    new(int32(0)),
		},
		{
			name: "timed out",
			status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type:    batchv1.JobFailed,
				Status:  corev1.ConditionTrue,
				Reason:  "DeadlineExceeded",
				Message: "Job was active longer than specified deadline",
			}}},
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{ExitCode: 137},
				},
			},
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeTimedOut),
			wantReason:  "DeadlineExceeded",
			wantExit:    new(int32(137)),
		},
		{
			name: "deadline termination still running",
			status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobFailureTarget,
				Status: corev1.ConditionTrue,
				Reason: "DeadlineExceeded",
			}}},
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{},
				},
			},
			wantPhase: api.RunObservationPhaseRunning,
		},
		{
			name: "deadline termination before Job failed condition",
			status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobFailureTarget,
				Status: corev1.ConditionTrue,
				Reason: "DeadlineExceeded",
			}}},
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{ExitCode: 143},
				},
			},
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeTimedOut),
			wantReason:  "DeadlineExceeded",
			wantExit:    new(int32(143)),
		},
		{
			name: "deadline termination overrides successful signal handler",
			status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobFailureTarget,
				Status: corev1.ConditionTrue,
				Reason: "DeadlineExceeded",
			}}},
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
				},
			},
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeTimedOut),
			wantReason:  "DeadlineExceeded",
			wantExit:    new(int32(0)),
		},
		{
			name: "deadline arrives between Job and Pod reads",
			statusAfterPodExit: &batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobFailureTarget,
				Status: corev1.ConditionTrue,
				Reason: "DeadlineExceeded",
			}}},
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
				},
			},
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeTimedOut),
			wantReason:  "DeadlineExceeded",
			wantExit:    new(int32(0)),
		},
		{
			name: "pending Job failure overrides successful signal handler",
			status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobFailureTarget,
				Status: corev1.ConditionTrue,
				Reason: "PodFailurePolicy",
			}}},
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
				},
			},
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeFailed),
			wantReason:  "PodFailurePolicy",
			wantExit:    new(int32(0)),
		},
		{
			name: "action exited",
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						ExitCode: 17,
						Reason:   "Error",
					},
				},
			},
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeFailed),
			wantReason:  "Error",
			wantExit:    new(int32(17)),
		},
		{
			name:     "canceled action exited",
			canceled: true,
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						ExitCode: 143,
						Reason:   "Error",
					},
				},
			},
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeCanceled),
			wantReason:  "CancelRequested",
			wantExit:    new(int32(143)),
		},
		{
			name:        "missing Job",
			missing:     true,
			handleUID:   "job-uid",
			wantPhase:   api.RunObservationPhaseFinished,
			wantOutcome: new(api.RunObservationOutcomeFailed),
			wantReason:  "JobNotFound",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assignment := testRunAssignment(t)
			job := testExecutionJob(t, assignment)
			job.Status = test.status
			objects := []runtime.Object{}
			if !test.missing {
				objects = append(objects, job)
			}
			if test.podStatus != nil {
				pod := testExecutionPod(job)
				pod.Status = corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{*test.podStatus},
				}
				if test.wantImages.Harness != nil {
					pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
						{Name: harnessContainerName, ImageID: *test.wantImages.Harness},
						{Name: checkoutContainerName, ImageID: *test.wantImages.Checkout},
					}
				}
				if test.canceled {
					pod.Annotations = map[string]string{
						cancellationAnnotation: "true",
					}
				}
				objects = append(objects, pod)
			}
			client := fake.NewClientset(objects...)
			if test.statusAfterPodExit != nil {
				jobReads := 0
				client.PrependReactor("get", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
					jobReads++
					if jobReads == 1 {
						return false, nil, nil
					}
					updated := job.DeepCopy()
					updated.Status = *test.statusAfterPodExit
					return true, updated, nil
				})
			}
			jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())

			observation, err := jobs.Observe(
				context.Background(),
				jobHandle{
					runID: assignment.ID, namespace: job.Namespace, name: job.Name,
					uid: test.handleUID,
				},
			)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			if observation.phase != test.wantPhase ||
				!equalPointers(observation.outcome, test.wantOutcome) ||
				valueOrEmpty(observation.jobUID) != "job-uid" ||
				valueOrEmpty(observation.reason) != test.wantReason ||
				!equalPointers(observation.exitCode, test.wantExit) ||
				!equalExecutionImages(observation.images, test.wantImages) {
				t.Errorf("Observe() = %#v", observation)
			}
		})
	}
}

func TestKubernetesRunJobsRejectsConflictingJob(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	name := "slipramp-run-" + assignment.ID.String()
	client := fake.NewClientset(&batchv1.Job{Name: name, Namespace: "slipramp-runs"})
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())

	_, err := jobs.Ensure(context.Background(), assignment)
	if err == nil || !strings.Contains(err.Error(), "expected Run identity") {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !isPermanentMaterializationError(err) {
		t.Fatalf("Ensure() error = %T, want permanent materialization error", err)
	}
}

func TestKubernetesRunJobsRecoversValidClusterJobs(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	job := &batchv1.Job{}
	job.Name = runJobName(runID)
	job.Namespace = "slipramp-runs"
	job.UID = types.UID("job-uid")
	job.Labels = map[string]string{
		managedByLabel: managedByLabelValue,
		clusterLabel:   "colo",
		runIDLabel:     runID.String(),
	}
	otherCluster := job.DeepCopy()
	otherCluster.Name = "other-cluster"
	otherCluster.Labels = map[string]string{
		managedByLabel: managedByLabelValue,
		clusterLabel:   "edge",
		runIDLabel:     uuid.NewString(),
	}
	invalidIdentity := job.DeepCopy()
	invalidIdentity.Name = "not-a-run-job"
	secondRunID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	secondNamespace := job.DeepCopy()
	secondNamespace.Name = runJobName(secondRunID)
	secondNamespace.Namespace = "slipramp-runs-secondary"
	secondNamespace.Labels[runIDLabel] = secondRunID.String()
	client := fake.NewClientset(job, otherCluster, invalidIdentity, secondNamespace)
	config := validConfig()
	config.ExecutionNamespaces = []string{
		"slipramp-runs",
		"slipramp-runs-secondary",
	}
	jobs := newKubernetesRunJobs(config, client.BatchV1(), client.CoreV1())

	handles, err := jobs.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(handles) != 2 || handles[0].runID != runID ||
		handles[0].namespace != job.Namespace || handles[0].uid != "job-uid" {
		t.Fatalf("Recover() = %#v", handles)
	}
	if handles[1].runID != secondRunID ||
		handles[1].namespace != secondNamespace.Namespace {
		t.Fatalf("Recover() second handle = %#v", handles[1])
	}
}

func TestKubernetesRunJobsRejectsUnownedExecutionNamespace(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	assignment.Placement.JobNamespace = "other-runs"
	client := fake.NewClientset()
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())

	_, err := jobs.Ensure(context.Background(), assignment)
	if err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("Ensure() error = %v, want unowned namespace", err)
	}
	if !isPermanentMaterializationError(err) {
		t.Fatalf("Ensure() error = %T, want permanent materialization error", err)
	}
	if errors.Is(err, errRunJobIdentityMismatch) || len(client.Actions()) != 0 {
		t.Fatal("invalid assignment was treated as an observed Job identity conflict")
	}
}

func TestKubernetesRunJobsRejectsIdentityChangesAcrossLifecycle(t *testing.T) {
	t.Parallel()

	changes := map[string]func(*batchv1.Job, *jobHandle){
		"replacement UID":  func(job *batchv1.Job, _ *jobHandle) { job.UID = "replacement-uid" },
		"managed-by label": func(job *batchv1.Job, _ *jobHandle) { delete(job.Labels, managedByLabel) },
		"Run label":        func(job *batchv1.Job, _ *jobHandle) { job.Labels[runIDLabel] = uuid.NewString() },
		"Cluster label":    func(job *batchv1.Job, _ *jobHandle) { job.Labels[clusterLabel] = "other" },
		"unowned namespace": func(_ *batchv1.Job, handle *jobHandle) {
			handle.namespace = "other-runs"
		},
		"unexpected name": func(_ *batchv1.Job, handle *jobHandle) { handle.name = "other-job" },
	}
	operations := map[string]func(*kubernetesRunJobs, jobHandle) error{
		"observe": func(jobs *kubernetesRunJobs, handle jobHandle) error {
			_, err := jobs.Observe(context.Background(), handle)
			return err
		},
		"log": func(jobs *kubernetesRunJobs, handle jobHandle) error {
			_, err := jobs.OpenLog(context.Background(), handle, false)
			return err
		},
		"cancel": func(jobs *kubernetesRunJobs, handle jobHandle) error {
			_, err := jobs.RequestCancellation(context.Background(), api.CancelRunCommand{
				ID: handle.runID, JobNamespace: api.CatalogName(handle.namespace), JobUID: new(handle.uid),
			})
			return err
		},
		"delete": func(jobs *kubernetesRunJobs, handle jobHandle) error {
			return jobs.Delete(context.Background(), handle)
		},
	}

	for changeName, change := range changes {
		for operationName, operation := range operations {
			if changeName == "unexpected name" && operationName == "cancel" {
				continue // Cancellation derives the Job name from the Run ID.
			}
			t.Run(changeName+"/"+operationName, func(t *testing.T) {
				t.Parallel()
				assignment := testRunAssignment(t)
				job := testExecutionJob(t, assignment)
				handle := handleFromJob(job, assignment.ID)
				change(job, &handle)
				client := fake.NewClientset(job)
				jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())

				if err := operation(jobs, handle); !errors.Is(err, errRunJobIdentityMismatch) {
					t.Fatalf("operation error = %v, want identity mismatch", err)
				}
				for _, action := range client.Actions() {
					if action.GetVerb() == "delete" || action.GetVerb() == "update" || action.GetSubresource() == "log" {
						t.Fatalf("identity mismatch allowed action %s %s", action.GetVerb(), action.GetResource().Resource)
					}
				}
			})
		}
	}
}

func TestKubernetesRunJobsRejectsPodWithConflictingOwner(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job := testExecutionJob(t, assignment)
	pod := testExecutionPod(job)
	pod.OwnerReferences[0].UID = "other-job-uid"
	client := fake.NewClientset(job, pod)
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())
	handle := handleFromJob(job, assignment.ID)

	_, err := jobs.Observe(context.Background(), handle)
	if !errors.Is(err, errRunJobIdentityMismatch) {
		t.Fatalf("Observe() error = %v, want identity mismatch", err)
	}
	_, err = jobs.OpenLog(context.Background(), handle, false)
	if !errors.Is(err, errRunJobIdentityMismatch) {
		t.Fatalf("OpenLog() error = %v, want identity mismatch", err)
	}
	_, err = jobs.RequestCancellation(context.Background(), api.CancelRunCommand{
		ID: assignment.ID, JobNamespace: assignment.Placement.JobNamespace,
	})
	if !errors.Is(err, errRunJobIdentityMismatch) {
		t.Fatalf("RequestCancellation() error = %v, want identity mismatch", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" || action.GetVerb() == "update" || action.GetSubresource() == "log" {
			t.Fatalf("conflicting Pod owner allowed action %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}

func TestKubernetesRunJobsSelectsPodsByPinnedJobUID(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job := testExecutionJob(t, assignment)
	pod := testExecutionPod(job)
	pod.CreationTimestamp = metav1.Now()
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: actionContainerName,
		State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{},
		},
	}}
	oldPod := testExecutionPod(job)
	oldPod.Name = "old-pod"
	oldPod.Labels[batchv1.ControllerUidLabel] = "old-job-uid"
	oldPod.OwnerReferences[0].UID = "old-job-uid"
	oldPod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: actionContainerName,
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{},
		},
	}}
	client := fake.NewClientset(job, pod, oldPod)
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())
	handle := handleFromJob(job, assignment.ID)

	observation, err := jobs.Observe(context.Background(), handle)
	if err != nil || observation.phase != api.RunObservationPhaseRunning {
		t.Fatalf("Observe() = %#v, %v; want running", observation, err)
	}
	stream, err := jobs.OpenLog(context.Background(), handle, false)
	if err != nil {
		t.Fatalf("OpenLog() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	contents, err := io.ReadAll(stream)
	if err != nil || string(contents) != "fake logs" {
		t.Fatalf("read logs = %q, %v", contents, err)
	}
	result, err := jobs.RequestCancellation(context.Background(), api.CancelRunCommand{
		ID: assignment.ID, JobNamespace: assignment.Placement.JobNamespace,
	})
	if err != nil || !result.jobRetained || result.handle.uid != string(job.UID) {
		t.Fatalf("RequestCancellation() = %#v, %v", result, err)
	}
	for _, expected := range []*corev1.Pod{pod, oldPod} {
		got, err := client.CoreV1().Pods(job.Namespace).Get(context.Background(), expected.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get Pod: %v", err)
		}
		if (got.Annotations[cancellationAnnotation] == "true") != (expected.Name == pod.Name) {
			t.Errorf("Pod %s cancellation annotation = %#v", got.Name, got.Annotations)
		}
	}
}

func TestKubernetesRunJobsReadsPinnedPodLogAfterJobDisappears(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job := testExecutionJob(t, assignment)
	client := fake.NewClientset(testExecutionPod(job))
	jobs := newKubernetesRunJobs(validConfig(), client.BatchV1(), client.CoreV1())
	stream, err := jobs.OpenLog(context.Background(), handleFromJob(job, assignment.ID), false)
	if err != nil {
		t.Fatalf("OpenLog() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	contents, err := io.ReadAll(stream)
	if err != nil || string(contents) != "fake logs" {
		t.Fatalf("read logs = %q, %v", contents, err)
	}
}

func testExecutionJob(t *testing.T, assignment api.RunAssignment) *batchv1.Job {
	t.Helper()

	job, err := renderJob(validConfig(), assignment)
	if err != nil {
		t.Fatalf("renderJob() error = %v", err)
	}
	job.UID = "job-uid"

	return job
}

func testExecutionPod(job *batchv1.Job) *corev1.Pod {
	pod := &corev1.Pod{}
	pod.Name = "run-pod"
	pod.Namespace = job.Namespace
	pod.Labels = maps.Clone(job.Labels)
	pod.Labels[batchv1.JobNameLabel] = job.Name
	pod.Labels[batchv1.ControllerUidLabel] = string(job.UID)
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: new(true),
	}}

	return pod
}
