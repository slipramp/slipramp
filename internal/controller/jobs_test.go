package controller

import (
	"context"
	"errors"
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
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)
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

	jobList, err := client.BatchV1().Jobs("slipramp-runs").List(
		context.Background(),
		metav1.ListOptions{},
	)
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
	pod := &corev1.Pod{}
	pod.Name = "run-pod"
	pod.Namespace = job.Namespace
	pod.Labels = map[string]string{
		batchv1.JobNameLabel: job.Name,
		managedByLabel:       managedByLabelValue,
		runIDLabel:           assignment.ID.String(),
		clusterLabel:         validConfig().ClusterName,
	}
	pod.Annotations = map[string]string{cancellationAnnotation: "false"}
	client := fake.NewClientset(job, pod)
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)
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
	gotPod, err := client.CoreV1().Pods(pod.Namespace).Get(
		context.Background(),
		pod.Name,
		metav1.GetOptions{},
	)
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
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)
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
	_, err = client.BatchV1().Jobs(job.Namespace).Get(
		context.Background(),
		job.Name,
		metav1.GetOptions{},
	)
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
	pod := &corev1.Pod{
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
				},
			},
		}},
	}
	pod.Name = "run-pod"
	pod.Namespace = job.Namespace
	pod.Labels = job.Spec.Template.Labels
	pod.Annotations = map[string]string{cancellationAnnotation: "false"}
	pod.Labels[batchv1.JobNameLabel] = job.Name
	client := fake.NewClientset(job, pod)
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)

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
	got, err := client.CoreV1().Pods(pod.Namespace).Get(
		context.Background(),
		pod.Name,
		metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("get completed Pod: %v", err)
	}
	if got.Annotations[cancellationAnnotation] != "false" {
		t.Errorf("cancellation annotation = %#v", got.Annotations)
	}
}

func TestKubernetesRunJobsDeleteUsesUIDPrecondition(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	replacement := &batchv1.Job{}
	replacement.Name = runJobName(runID)
	replacement.Namespace = "slipramp-runs"
	replacement.UID = types.UID("replacement-uid")
	client := fake.NewClientset(replacement)
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

			return true, nil, apierrors.NewConflict(
				schema.GroupResource{Group: "batch", Resource: "jobs"},
				replacement.Name,
				errors.New("UID precondition failed"),
			)
		},
	)
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)
	handle := jobHandle{
		runID:     runID,
		namespace: replacement.Namespace,
		name:      replacement.Name,
		uid:       "original-uid",
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

	runID := uuid.New()
	job := &batchv1.Job{}
	job.Name = runJobName(runID)
	job.Namespace = "slipramp-runs"
	job.UID = types.UID("job-uid")
	client := fake.NewClientset(job)
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)
	handle := handleFromJob(job, runID)

	if err := jobs.Delete(context.Background(), handle); err != nil {
		t.Fatalf("first Delete() error = %v", err)
	}
	_, err := client.BatchV1().Jobs(job.Namespace).Get(
		context.Background(),
		job.Name,
		metav1.GetOptions{},
	)
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
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)

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
		name        string
		missing     bool
		status      batchv1.JobStatus
		podStatus   *corev1.ContainerStatus
		canceled    bool
		handleUID   string
		wantPhase   api.RunObservationPhase
		wantOutcome *api.RunObservationOutcome
		wantReason  string
		wantExit    *int32
	}{
		{
			name:      "active Job with pending action",
			status:    batchv1.JobStatus{Active: 1},
			wantPhase: api.RunObservationPhasePreparing,
		},
		{
			name: "running",
			podStatus: &corev1.ContainerStatus{
				Name: actionContainerName,
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{},
				},
			},
			wantPhase: api.RunObservationPhaseRunning,
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
			job := &batchv1.Job{
				Name:      runJobName(assignment.ID),
				Namespace: string(assignment.Placement.JobNamespace),
				Status:    test.status,
			}
			objects := []runtime.Object{}
			if !test.missing {
				objects = append(objects, job)
			}
			if test.podStatus != nil {
				pod := &corev1.Pod{
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{*test.podStatus},
					},
				}
				pod.Name = "run-pod"
				pod.Namespace = job.Namespace
				pod.Labels = map[string]string{batchv1.JobNameLabel: job.Name}
				if test.canceled {
					pod.Annotations = map[string]string{
						cancellationAnnotation: "true",
					}
				}
				objects = append(objects, pod)
			}
			client := fake.NewClientset(objects...)
			jobs := newKubernetesRunJobs(
				validConfig(),
				client.BatchV1(),
				client.CoreV1(),
			)

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
				valueOrEmpty(observation.reason) != test.wantReason ||
				!equalPointers(observation.exitCode, test.wantExit) {
				t.Errorf("Observe() = %#v", observation)
			}
		})
	}
}

func TestKubernetesRunJobsRejectsConflictingJob(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	name := "slipramp-run-" + assignment.ID.String()
	client := fake.NewClientset(&batchv1.Job{
		Name:      name,
		Namespace: "slipramp-runs",
	})
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)

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
	jobs := newKubernetesRunJobs(
		config,
		client.BatchV1(),
		client.CoreV1(),
	)

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
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)

	_, err := jobs.Ensure(context.Background(), assignment)
	if err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("Ensure() error = %v, want unowned namespace", err)
	}
	if !isPermanentMaterializationError(err) {
		t.Fatalf("Ensure() error = %T, want permanent materialization error", err)
	}
}
