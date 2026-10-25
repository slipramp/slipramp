package catalog_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

func TestExplainPortableDefaultsAndParameters(t *testing.T) {
	t.Parallel()

	result := processExplanationCatalog(t, nil)
	explanation, err := result.Explain("network", "plan", map[string]string{
		"a-force": "true", "b-positional": "false", "c-count": "1e2", "d-mode": "safe", "z-version": "a=b",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertExplained(t, explanation.Runtime.Name, "tools", "target-default")
	assertExplained(t, explanation.Timeout, "45m", "target-default")
	assertExplained(t, explanation.Runtime.Image, "example.com/tools:1", "runtime-declaration")
	assertExplained(t, explanation.Process.RepositoryMount, "/repository", "runtime-declaration")
	if explanation.Process.WorkingDirectory.Value != "/repository/infrastructure/network" ||
		len(explanation.Process.WorkingDirectory.Provenance) != 2 {
		t.Errorf("process directory = %#v", explanation.Process.WorkingDirectory)
	}
	wantArgv := []string{"terraform", "plan", "--force", "false", "--count", "100", "--mode", "safe"}
	if !reflect.DeepEqual(explanation.Process.Argv.Value, wantArgv) {
		t.Errorf("argv = %#v, want %#v", explanation.Process.Argv.Value, wantArgv)
	}
	if explanation.Process.Environment["VERSION"].Value != "a=b" ||
		explanation.Process.Environment["TF_IN_AUTOMATION"].Value != "true" {
		t.Errorf("process environment = %#v", explanation.Process.Environment)
	}
	if len(explanation.Parameters) != 6 || explanation.Parameters[4].Name != "e-optional" ||
		explanation.Parameters[4].Supplied || len(explanation.Parameters[4].Delivery.Value.Arguments) != 0 {
		t.Errorf("parameters = %#v", explanation.Parameters)
	}
	if explanation.Parameters[0].Delivery.Provenance[1].Kind != "cli-parameter" {
		t.Errorf("parameter provenance = %#v", explanation.Parameters[0])
	}
	if explanation.Placement.ConfigurationAvailable || explanation.Placement.Cluster.Resolved ||
		explanation.Placement.Namespace.Resolved || explanation.Placement.ServiceAccount.Resolved ||
		explanation.Resources.PlacementDefaultsResolved || explanation.Resources.UnresolvedReason == "" {
		t.Errorf("portable Placement = %#v, resources = %#v", explanation.Placement, explanation.Resources)
	}
	credential := explanation.Credentials[0]
	assertExplained(t, credential.Binding, "backend", "target-default")
	if credential.Source.Resolved || credential.Source.UnresolvedReason == "" ||
		credential.Injection.Value.Environment["AWS_ACCESS_KEY_ID"].Key != "accessKeyId" ||
		!credential.Injection.Value.EnvironmentFrom || credential.Injection.Value.Files[0].Mode != 0o400 ||
		credential.Injection.Value.Directory.Path != "/credentials/all" {
		t.Errorf("credential = %#v", credential)
	}
	if explanation.Target.Provenance[0].Location.Document != 2 ||
		explanation.Runtime.Image.Provenance[0].Location.Document != 1 ||
		explanation.Runtime.Image.Provenance[0].Location.Path != ".slipramp/nested/catalog.yml" {
		t.Errorf("source provenance = %#v, %#v", explanation.Target, explanation.Runtime.Image)
	}
	if len(explanation.Triggers) != 1 || explanation.Triggers[0].Value.Name != "changed" ||
		explanation.Triggers[0].Value.Parameters["z-version"] != "stable" ||
		!reflect.DeepEqual(explanation.Watch.Value, []string{"infrastructure/network/**"}) {
		t.Errorf("watch/triggers = %#v, %#v", explanation.Watch, explanation.Triggers)
	}
	if len(explanation.Outputs) != 1 || explanation.Outputs[0].ProducerAction != "plan" ||
		len(explanation.Outputs[0].Consumers) != 1 || explanation.Outputs[0].Consumers[0].Action != "apply" {
		t.Fatalf("outputs = %#v", explanation.Outputs)
	}
	assertExplained(t, explanation.Outputs[0].Consumers[0].Compatibility,
		catalog.ArtifactCompatibilityExactCatalogRevision, "system-default")
}

func TestExplainDeploymentOverridesResourcesAndInputs(t *testing.T) {
	t.Parallel()

	placements, err := placement.Decode(strings.NewReader(explanationPlacements))
	if err != nil {
		t.Fatal(err)
	}
	result := processExplanationCatalog(t, &placements)
	explanation, err := result.Explain("network", "apply", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertExplained(t, explanation.Runtime.Name, "alternate", "action-override")
	assertExplained(t, explanation.Placement.Name, "preview-cluster", "action-override")
	assertExplained(t, explanation.Timeout, "5m", "action-override")
	assertExplained(t, explanation.Placement.Cluster, "colo", "placement-default")
	assertExplained(t, explanation.Placement.Namespace, "slipramp-runs", "placement-default")
	assertExplained(t, explanation.Placement.ServiceAccount, "ops", "placement-default")
	assertExplained(t, explanation.Resources.Requests["cpu"], "500m", "action-override")
	assertExplained(t, explanation.Resources.Requests["memory"], "256Mi", "placement-default")
	assertExplained(t, explanation.Resources.Limits["cpu"], "2", "target-default")
	assertExplained(t, explanation.Resources.Limits["memory"], "1Gi", "action-override")
	assertExplained(t, explanation.Process.RepositoryMount, "/workspace", "system-default")
	if !explanation.Resources.PlacementDefaultsResolved || explanation.Resources.UnresolvedReason != "" {
		t.Errorf("resources = %#v", explanation.Resources)
	}
	credential := explanation.Credentials[0]
	assertExplained(t, credential.Binding, "alternate-backend", "action-override")
	if !credential.Source.Resolved || credential.Source.Value.SecretName != "terraform-backend-alternate" {
		t.Errorf("credential = %#v", credential)
	}
	if len(explanation.Inputs) != 1 || explanation.Inputs[0].Input.Value.Artifact.FromAction != "plan" ||
		explanation.Inputs[0].Input.Value.MountPath != "/inputs/plan" ||
		explanation.Inputs[0].Producer.Value.Path != "/outputs/plan" ||
		!explanation.Inputs[0].Producer.Value.Sensitive {
		t.Fatalf("inputs = %#v", explanation.Inputs)
	}
	assertExplained(t, explanation.Inputs[0].Compatibility,
		catalog.ArtifactCompatibilityExactCatalogRevision, "system-default")
	encoded, err := json.Marshal(explanation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "do-not-expose-pod-annotation") {
		t.Fatal("explanation exposed opaque pod configuration")
	}
}

func TestExplainPlacementDefaultsAndUnknownTimeout(t *testing.T) {
	t.Parallel()

	placements, err := placement.Decode(strings.NewReader(explanationPlacements))
	if err != nil {
		t.Fatal(err)
	}
	for _, configured := range []*placement.Set{nil, &placements} {
		result := processExplanationCatalog(t, configured)
		result.Catalog.Targets[0].Spec.Timeout = ""
		explanation, err := result.Explain("network", "plan", map[string]string{"z-version": "stable"})
		if err != nil {
			t.Fatal(err)
		}
		if configured == nil {
			if explanation.Timeout.Resolved || explanation.Timeout.UnresolvedReason == "" {
				t.Errorf("portable timeout = %#v", explanation.Timeout)
			}
		} else {
			assertExplained(t, explanation.Timeout, "1h0m0s", "placement-default")
			assertExplained(t, explanation.Resources.Limits["memory"], "2Gi", "placement-default")
			assertExplained(t, explanation.Placement.ServiceAccount, "default", "system-default")
		}
	}
}

func TestExplainRejectsInvalidSelectionAndParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, target, action, message string
		parameters                    map[string]string
	}{
		{name: "target", target: "absent", action: "plan", message: "unknown Target"},
		{name: "action", target: "network", action: "absent", message: "unknown Action"},
		{name: "required", target: "network", action: "plan", message: "was not supplied"},
		{
			name: "unknown", target: "network", action: "plan",
			parameters: map[string]string{"unknown": "x"}, message: "not declared",
		},
		{
			name: "boolean", target: "network", action: "plan",
			parameters: map[string]string{"a-force": "maybe"}, message: "must be a boolean",
		},
		{
			name: "enum", target: "network", action: "plan",
			parameters: map[string]string{"d-mode": "unsafe"}, message: "must be one of",
		},
		{
			name: "number", target: "network", action: "plan",
			parameters: map[string]string{"c-count": "abc"}, message: "finite number",
		},
		{
			name: "nan", target: "network", action: "plan",
			parameters: map[string]string{"c-count": "NaN"}, message: "finite number",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := processExplanationCatalog(t, nil)
			_, err := result.Explain(test.target, test.action, test.parameters)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("Explain() error = %v, want %q", err, test.message)
			}
		})
	}
}

func assertExplained(t *testing.T, value catalog.ExplainedValue[string], expected, originKind string) {
	t.Helper()
	if !value.Resolved || value.Value != expected || len(value.Provenance) != 1 || value.Provenance[0].Kind != originKind {
		t.Errorf("explained value = %#v, want %q from %s", value, expected, originKind)
	}
}

func processExplanationCatalog(t *testing.T, placements *placement.Set) catalog.Result {
	t.Helper()
	files := fstest.MapFS{
		".slipramp/nested/catalog.yml":   {Data: []byte(explanationCatalog)},
		"infrastructure/network/main.tf": {Data: []byte("")},
	}
	result, err := catalog.Process(files, catalog.Options{Placements: placements})
	if err != nil || result.Diagnostics.HasErrors() {
		t.Fatalf("Process() = %v, diagnostics = %s", err, result.Diagnostics.ErrorSummary())
	}

	return result
}

const explanationCatalog = `apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: tools
spec:
  image: example.com/tools:1
  workingDirectory: /repository
  environment:
    TF_IN_AUTOMATION: "true"
  credentials:
    backend:
      inject:
        environment:
          AWS_ACCESS_KEY_ID: {key: accessKeyId}
        environmentFrom: true
        files:
          - {key: token, path: /credentials/token, mode: 256}
        directory: {path: /credentials/all}
---
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: network
  title: Network infrastructure
spec:
  path: infrastructure/network
  watch: [infrastructure/network/**]
  runtime: tools
  placement: infrastructure-admin
  timeout: 45m
  credentials: {backend: backend}
  resources:
    requests: {cpu: 200m}
    limits: {cpu: "2"}
  actions:
    plan:
      intent: preview
      command: [terraform, plan]
      parameters:
        a-force: {type: boolean, argument: {name: --force}}
        b-positional: {type: boolean, argument: {}}
        c-count: {type: number, argument: {name: --count}}
        d-mode: {type: enum, values: [safe, fast], argument: {name: --mode}}
        e-optional: {type: string, argument: {}}
        z-version: {type: string, required: true, environment: VERSION}
      artifacts:
        plan: {path: /outputs/plan, mediaType: application/octet-stream, sensitive: true}
      triggers:
        - name: changed
          git: {}
          parameters: {z-version: stable}
    apply:
      intent: reconcile
      runtime: alternate
      placement: preview-cluster
      timeout: 5m
      credentials: {backend: alternate-backend}
      command: [terraform, apply, /inputs/plan]
      resources:
        requests: {cpu: 500m}
        limits: {memory: 1Gi}
      inputs:
        plan:
          artifact: {fromAction: plan}
          mountPath: /inputs/plan
---
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: alternate
spec:
  image: example.com/tools:2
  credentials:
    backend:
      inject:
        environment:
          AWS_ACCESS_KEY_ID: {key: accessKeyId}
`

const explanationPlacements = `apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  infrastructure-admin:
    cluster: colo
    jobNamespace: slipramp-runs
    timeout: 1h
    resources:
      requests: {memory: 256Mi}
      limits: {memory: 2Gi, cpu: "4"}
    credentials:
      backend: {secretRef: {name: terraform-backend}}
  preview-cluster:
    cluster: colo
    jobNamespace: slipramp-runs
    serviceAccountName: ops
    resources:
      requests: {memory: 256Mi}
      limits: {memory: 2Gi, cpu: "4"}
    nodeSelector: {kubernetes.io/arch: amd64}
    podTemplate:
      metadata:
        annotations: {private: do-not-expose-pod-annotation}
    credentials:
      alternate-backend: {secretRef: {name: terraform-backend-alternate}}
`
