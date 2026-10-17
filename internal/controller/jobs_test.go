package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/slipramp/slipramp/internal/api"
)

func TestRenderJobBuildsFrozenRun(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	job, err := renderJob(validConfig(), assignment)
	if err != nil {
		t.Fatalf("renderJob() error = %v", err)
	}

	wantName := "slipramp-run-018f90ff-9fd8-7b5f-b7e1-f32ff54c1200"
	if job.Name != wantName || job.Namespace != "slipramp-runs" {
		t.Errorf("Job identity = %s/%s, want slipramp-runs/%s", job.Namespace, job.Name, wantName)
	}
	if job.Labels[runIDLabel] != assignment.ID.String() ||
		job.Labels[clusterLabel] != "colo" ||
		job.Labels[managedByLabel] != managedByLabelValue {
		t.Errorf("Job labels = %#v", job.Labels)
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Errorf("backoff limit = %v, want 0", job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil ||
		*job.Spec.ActiveDeadlineSeconds != 1800 {
		t.Errorf("active deadline = %v, want 1800", job.Spec.ActiveDeadlineSeconds)
	}

	pod := job.Spec.Template
	if pod.Labels["example.com/placement"] != "admin" ||
		pod.Labels[runIDLabel] != assignment.ID.String() {
		t.Errorf("Pod labels = %#v", pod.Labels)
	}
	if pod.Spec.ServiceAccountName != "infrastructure-admin" {
		t.Errorf("service account = %q", pod.Spec.ServiceAccountName)
	}
	if pod.Spec.NodeSelector["topology.kubernetes.io/zone"] != "west" {
		t.Errorf("node selector = %#v", pod.Spec.NodeSelector)
	}
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "kata" {
		t.Errorf("runtime class = %v", pod.Spec.RuntimeClassName)
	}
	if len(pod.Spec.InitContainers) != 2 {
		t.Fatalf("init containers = %d, want 2", len(pod.Spec.InitContainers))
	}
	if pod.Spec.InitContainers[0].Image != validConfig().CheckoutImage ||
		pod.Spec.InitContainers[1].Image != validConfig().HarnessImage {
		t.Errorf("init containers = %#v", pod.Spec.InitContainers)
	}
	if !strings.Contains(pod.Spec.InitContainers[0].Args[0], "git init") ||
		pod.Spec.InitContainers[0].Args[4] != assignment.CommitSha {
		t.Errorf("checkout arguments = %#v", pod.Spec.InitContainers[0].Args)
	}

	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("containers = %d, want 1", len(pod.Spec.Containers))
	}
	action := pod.Spec.Containers[0]
	if action.Image != "registry.example.com/terraform:1" ||
		len(action.Command) != 1 || action.Command[0] != harnessExecutablePath {
		t.Errorf("action image/command = %q %#v", action.Image, action.Command)
	}
	wantArguments := []string{
		"terraform",
		"apply",
		"--force",
		"--strategy",
		"rolling",
	}
	if strings.Join(action.Args, "\x00") != strings.Join(wantArguments, "\x00") {
		t.Errorf("action arguments = %#v, want %#v", action.Args, wantArguments)
	}
	if action.WorkingDir != "/workspace/infrastructure/network" {
		t.Errorf("working directory = %q", action.WorkingDir)
	}
	if environmentValue(action.Env, "TF_IN_AUTOMATION") != "true" ||
		environmentValue(action.Env, "VERSION") != "1.2.3" {
		t.Errorf("environment = %#v", action.Env)
	}
	if action.Resources.Requests.Cpu().String() != "200m" ||
		action.Resources.Limits.Memory().String() != "1Gi" {
		t.Errorf("resources = %#v", action.Resources)
	}
	if len(action.EnvFrom) != 1 ||
		action.EnvFrom[0].SecretRef.Name != "ssh-deploy-key" {
		t.Errorf("envFrom = %#v", action.EnvFrom)
	}
	if !hasSecretEnvironment(action, "AWS_ACCESS_KEY_ID", "terraform-backend", "accessKeyId") {
		t.Errorf("credential environment = %#v", action.Env)
	}
	if !hasMount(action, "/home/ops/.ssh/id_ed25519", true) ||
		!hasMount(action, "/var/run/ssh-extensions", false) {
		t.Errorf("credential mounts = %#v", action.VolumeMounts)
	}
}

func TestJobMaterializerCreatesAndReusesJob(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	materializer := newJobMaterializer(validConfig(), client.BatchV1())
	assignment := testRunAssignment(t)

	created, err := materializer.Ensure(context.Background(), assignment)
	if err != nil {
		t.Fatalf("first Ensure() error = %v", err)
	}
	if !created {
		t.Error("first Ensure() created = false, want true")
	}

	created, err = materializer.Ensure(context.Background(), assignment)
	if err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
	if created {
		t.Error("second Ensure() created = true, want false")
	}

	jobs, err := client.BatchV1().Jobs("slipramp-runs").List(
		context.Background(),
		metav1.ListOptions{},
	)
	if err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Errorf("Jobs = %d, want 1", len(jobs.Items))
	}
}

func TestJobMaterializerRejectsConflictingJob(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	name := "slipramp-run-" + assignment.ID.String()
	client := fake.NewClientset(&batchv1.Job{
		Name:      name,
		Namespace: "slipramp-runs",
	})
	materializer := newJobMaterializer(validConfig(), client.BatchV1())

	_, err := materializer.Ensure(context.Background(), assignment)
	if err == nil || !strings.Contains(err.Error(), "expected Run identity") {
		t.Fatalf("Ensure() error = %v", err)
	}
}

func TestRenderJobRejectsUnknownGitCredential(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	config := validConfig()
	config.GitCredentials = map[string]string{}

	_, err := renderJob(config, assignment)
	if err == nil || !strings.Contains(err.Error(), "github-readonly") {
		t.Fatalf("renderJob() error = %v", err)
	}
}

func TestRenderJobRejectsDifferentCluster(t *testing.T) {
	t.Parallel()

	assignment := testRunAssignment(t)
	assignment.Placement.Cluster = "edge"

	_, err := renderJob(validConfig(), assignment)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("renderJob() error = %v", err)
	}
}

func testRunAssignment(t *testing.T) api.RunAssignment {
	t.Helper()

	input := `{
  "id":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
  "catalogRevisionId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
  "commitSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "parameters":{"force":true,"strategy":"rolling","version":"1.2.3"},
  "repository":{
    "name":"operations",
    "cloneUrl":"ssh://git@example.com/operations.git",
    "branch":"main",
    "checkoutCredentialName":"github-readonly"
  },
  "target":{"name":"network","path":"infrastructure/network"},
  "action":{
    "name":"sync",
    "command":["terraform","apply"],
    "runtime":"terraform",
    "placement":"admin",
    "timeout":"30m",
    "credentials":{"backend":"terraform-backend","ssh":"ssh-deploy-key"},
    "resources":{"requests":{"cpu":"200m"}},
    "parameterDefinitions":{
      "force":{"type":"boolean","argument":{"name":"--force"}},
      "strategy":{"type":"enum","values":["rolling","immediate"],"argument":{"name":"--strategy"}},
      "version":{"type":"string","environment":"VERSION"}
    }
  },
  "runtime":{
    "name":"terraform",
    "image":"registry.example.com/terraform:1",
    "workingDirectory":"/workspace",
    "environment":{"TF_IN_AUTOMATION":"true"},
    "credentials":{
      "backend":{"inject":{"environment":{"AWS_ACCESS_KEY_ID":{"key":"accessKeyId"}}}},
      "ssh":{"inject":{
        "environmentFrom":true,
        "files":[{"key":"privateKey","path":"/home/ops/.ssh/id_ed25519","mode":256}],
        "directory":{"path":"/var/run/ssh-extensions","mode":292}
      }}
    }
  },
  "placement":{
    "name":"admin",
    "cluster":"colo",
    "jobNamespace":"slipramp-runs",
    "serviceAccountName":"infrastructure-admin",
    "timeout":"1h",
    "resources":{"requests":{"cpu":"100m"},"limits":{"memory":"1Gi"}},
    "nodeSelector":{"topology.kubernetes.io/zone":"west"},
    "credentials":{
      "terraform-backend":{"secretName":"terraform-backend"},
      "ssh-deploy-key":{"secretName":"ssh-deploy-key"}
    },
    "podTemplate":{"metadata":{"labels":{"example.com/placement":"admin"}},"spec":{"runtimeClassName":"kata"}}
  }
}`

	var assignment api.RunAssignment
	if err := json.Unmarshal([]byte(input), &assignment); err != nil {
		t.Fatalf("decode test Run assignment: %v", err)
	}

	return assignment
}

func environmentValue(environment []corev1.EnvVar, name string) string {
	for _, variable := range environment {
		if variable.Name == name {
			return variable.Value
		}
	}

	return ""
}

func hasSecretEnvironment(
	container corev1.Container,
	name string,
	secretName string,
	key string,
) bool {
	for _, variable := range container.Env {
		if variable.Name == name && variable.ValueFrom != nil &&
			variable.ValueFrom.SecretKeyRef != nil &&
			variable.ValueFrom.SecretKeyRef.Name == secretName &&
			variable.ValueFrom.SecretKeyRef.Key == key {
			return true
		}
	}

	return false
}

func hasMount(container corev1.Container, mountPath string, subPath bool) bool {
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == mountPath && (mount.SubPath != "") == subPath {
			return true
		}
	}

	return false
}
