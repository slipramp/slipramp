package controller

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/slipramp/slipramp/internal/api"
)

func TestRenderPlacementPreflightJobUsesPlacementExecutionPolicy(t *testing.T) {
	t.Parallel()

	command := testPlacementPreflightCommand()
	command.ServiceAccountName = new("infrastructure-admin")
	command.NodeSelector = &map[string]string{"workload": "infrastructure"}
	command.Resources = &api.ResourceRequirements{
		Requests: &map[string]string{"cpu": "100m"},
		Limits:   &map[string]string{"memory": "256Mi"},
	}
	command.PodTemplate = &map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"example.com/policy": "probe"}},
		"spec":     map[string]any{"runtimeClassName": "kata"},
	}

	job, err := renderPlacementPreflightJob(validConfig(), command)
	if err != nil {
		t.Fatalf("renderPlacementPreflightJob() error = %v", err)
	}
	if job.Name != placementPreflightJobName(command.ID) || job.Namespace != "slipramp-runs" ||
		job.Labels[preflightIDLabel] != command.ID.String() {
		t.Errorf("Job identity = %#v", job.ObjectMeta)
	}
	pod := job.Spec.Template
	if pod.Spec.ServiceAccountName != "infrastructure-admin" || pod.Spec.RuntimeClassName == nil ||
		*pod.Spec.RuntimeClassName != "kata" || pod.Spec.NodeSelector["workload"] != "infrastructure" ||
		pod.Labels["example.com/policy"] != "probe" {
		t.Errorf("Pod policy = %#v", pod)
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != validConfig().CheckoutImage ||
		pod.Spec.Containers[0].Resources.Requests.Cpu().String() != "100m" ||
		pod.Spec.Containers[0].Resources.Limits.Memory().String() != "256Mi" {
		t.Errorf("probe container = %#v", pod.Spec.Containers)
	}
}

func TestValidatePlacementPreflightJobPinsIdentity(t *testing.T) {
	t.Parallel()

	command := testPlacementPreflightCommand()
	job, err := renderPlacementPreflightJob(validConfig(), command)
	if err != nil {
		t.Fatalf("renderPlacementPreflightJob() error = %v", err)
	}
	job.UID = "preflight-job-uid"
	handle := placementPreflightHandle{
		id: command.ID, namespace: job.Namespace, name: job.Name, uid: string(job.UID),
	}
	tests := []struct {
		name   string
		mutate func(*batchv1.Job)
	}{
		{name: "different Cluster", mutate: func(job *batchv1.Job) { job.Labels[clusterLabel] = "other" }},
		{name: "missing admitted UID", mutate: func(job *batchv1.Job) { job.UID = "" }},
		{name: "replacement UID", mutate: func(job *batchv1.Job) { job.UID = "replacement-uid" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			candidate := job.DeepCopy()
			test.mutate(candidate)
			if err := validatePlacementPreflightJob("colo", handle, candidate); err == nil {
				t.Fatal("validatePlacementPreflightJob() error = nil, want identity mismatch")
			}
		})
	}
}

func TestKubernetesPlacementPreflightJobsObserveWarningsAndCleanup(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	client.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		job := action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		job.UID = types.UID("preflight-job-uid")
		return false, nil, nil
	})
	jobs := newKubernetesPlacementPreflightJobs(validConfig(), client.BatchV1(), client.CoreV1())
	command := testPlacementPreflightCommand()
	handle, err := jobs.Ensure(context.Background(), command)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if handle.uid != "preflight-job-uid" {
		t.Fatalf("handle = %#v", handle)
	}

	job, err := client.BatchV1().Jobs(handle.namespace).Get(context.Background(), handle.name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get Job: %v", err)
	}
	job.Status.Conditions = []batchv1.JobCondition{{
		Type: batchv1.JobComplete, Status: corev1.ConditionTrue,
	}}
	if _, err := client.BatchV1().Jobs(handle.namespace).Update(context.Background(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update Job: %v", err)
	}
	event := &corev1.Event{
		InvolvedObject: corev1.ObjectReference{UID: types.UID(handle.uid)},
		Type:           corev1.EventTypeWarning, Reason: "AdmissionNotice", Message: "policy emitted a warning", Count: 2,
		FirstTimestamp: metav1.NewTime(time.Now().Add(-time.Second)),
		LastTimestamp:  metav1.NewTime(time.Now()),
	}
	event.Name = "probe-warning"
	event.Namespace = handle.namespace
	_, err = client.CoreV1().Events(handle.namespace).Create(context.Background(), event, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create Event: %v", err)
	}

	observation, err := jobs.Observe(context.Background(), handle)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if !observation.finished || !observation.succeeded || observation.reason != "ProbeSucceeded" ||
		len(observation.diagnostics) != 1 || observation.diagnostics[0].Count != 2 {
		t.Errorf("observation = %#v", observation)
	}
	if err := jobs.Delete(context.Background(), handle); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func testPlacementPreflightCommand() api.PlacementPreflightCommand {
	return api.PlacementPreflightCommand{
		ID: uuid.New(), PlacementName: "infrastructure", ConfigurationChecksum: "checksum",
		Cluster: "colo", JobNamespace: "slipramp-runs", TimeoutSeconds: 120,
	}
}
