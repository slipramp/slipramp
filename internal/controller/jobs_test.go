package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

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

func TestKubernetesRunJobsObservesJobState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		missing     bool
		status      batchv1.JobStatus
		podStatus   *corev1.ContainerStatus
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
	client := fake.NewClientset(job, otherCluster, invalidIdentity)
	jobs := newKubernetesRunJobs(
		validConfig(),
		client.BatchV1(),
		client.CoreV1(),
	)

	handles, err := jobs.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(handles) != 1 || handles[0].runID != runID ||
		handles[0].namespace != job.Namespace || handles[0].uid != "job-uid" {
		t.Fatalf("Recover() = %#v", handles)
	}
}
