package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/testcheck"
)

const serverPlacementsFileEnvironment = "SLIPRAMP_PLACEMENTS_FILE"

func TestLoadServerPlacementsFile(t *testing.T) {
	t.Parallel()

	path := writePlacementsFile(t, `
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  infrastructure-admin:
    cluster: colo
    jobNamespace: slipramp-runs
    serviceAccountName: infrastructure-admin
    timeout: 30m
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: "2"
        memory: 2Gi
    nodeSelector:
      topology.kubernetes.io/zone: us-west-2a
    credentials:
      terraform-backend:
        secretRef:
          name: terraform-backend
    podTemplate:
      metadata:
        labels:
          azure.workload.identity/use: "true"
      spec:
        runtimeClassName: kata
        volumes:
          - name: workload-identity
            projected:
              sources:
                - serviceAccountToken:
                    audience: vault
                    path: token
`)
	environment := map[string]string{
		serverControllerEnrollmentTokensEnvironment: `{"colo":"enrollment-token"}`,
		serverPlacementsFileEnvironment:             path,
		serverOIDCIssuerEnvironment:                 "https://identity.example.com",
		serverOIDCClientIDEnvironment:               "slipramp",
		serverOIDCClientSecretEnvironment:           "client-secret",
		serverOIDCRedirectURLEnvironment:            "https://slipramp.example.com/api/v1/auth/callback",
		serverOIDCSubjectRolesEnvironment:           `{"admin-subject":"admin"}`,
	}

	cfg, err := config.LoadServer(mapEnvironment(environment))
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}

	configured, ok := cfg.Placements.Lookup("infrastructure-admin")
	if !ok {
		t.Fatal("Placements.Lookup() found = false, want true")
	}

	if configured.ClusterName != "colo" {
		t.Errorf("ClusterName = %q, want %q", configured.ClusterName, "colo")
	}

	if configured.JobNamespace != "slipramp-runs" {
		t.Errorf(
			"JobNamespace = %q, want %q",
			configured.JobNamespace,
			"slipramp-runs",
		)
	}

	if configured.ServiceAccountName != "infrastructure-admin" {
		t.Errorf(
			"ServiceAccountName = %q, want %q",
			configured.ServiceAccountName,
			"infrastructure-admin",
		)
	}

	if configured.Timeout != 30*time.Minute {
		t.Errorf("Timeout = %s, want %s", configured.Timeout, 30*time.Minute)
	}

	if configured.Resources.Requests["cpu"] != "100m" {
		t.Errorf(
			"Resources.Requests[cpu] = %q, want %q",
			configured.Resources.Requests["cpu"],
			"100m",
		)
	}

	if configured.Resources.Limits["memory"] != "2Gi" {
		t.Errorf(
			"Resources.Limits[memory] = %q, want %q",
			configured.Resources.Limits["memory"],
			"2Gi",
		)
	}

	if configured.NodeSelector["topology.kubernetes.io/zone"] != "us-west-2a" {
		t.Error("NodeSelector does not contain the configured zone")
	}

	credential := configured.Credentials["terraform-backend"]
	if credential.SecretRef.Name != "terraform-backend" {
		t.Errorf(
			"credential SecretRef.Name = %q, want %q",
			credential.SecretRef.Name,
			"terraform-backend",
		)
	}

	if configured.PodTemplate == nil {
		t.Fatal("PodTemplate = nil, want configured template")
	}

	if configured.PodTemplate.Spec.RuntimeClassName == nil ||
		*configured.PodTemplate.Spec.RuntimeClassName != "kata" {
		t.Error("PodTemplate.Spec.RuntimeClassName does not contain kata")
	}
}

func TestLoadServerRejectsInvalidPlacementsFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		contents   string
		wantError  string
		registered bool
	}{
		{
			name:      "malformed YAML",
			contents:  "placements: [",
			wantError: "decode placement configuration",
		},
		{
			name: "unknown field",
			contents: validPlacementsFile(`
    nodeSelecter:
      region: west`),
			wantError: "nodeSelecter",
		},
		{
			name: "duplicate placement",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  infrastructure:
    cluster: colo
    jobNamespace: slipramp-runs
  infrastructure:
    cluster: edge
    jobNamespace: slipramp-runs
`,
			wantError: "already defined",
		},
		{
			name: "missing API version",
			contents: `
kind: PlacementConfig
placements: {}
`,
			wantError: "apiVersion",
		},
		{
			name: "unsupported API version",
			contents: `
apiVersion: slipramp.com/v2
kind: PlacementConfig
placements: {}
`,
			wantError: "apiVersion",
		},
		{
			name: "wrong kind",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: Placements
placements: {}
`,
			wantError: "kind",
		},
		{
			name: "missing placements",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
`,
			wantError: "must define placements",
		},
		{
			name: "multiple documents",
			contents: validPlacementsFile("") + `
---
placements: {}
`,
			wantError: "exactly one document",
		},
		{
			name:      "malformed timeout",
			contents:  validPlacementsFile("    timeout: later"),
			wantError: "timeout",
		},
		{
			name:      "non-positive timeout",
			contents:  validPlacementsFile("    timeout: 0s"),
			wantError: "must be positive",
		},
		{
			name: "invalid placement name",
			contents: `
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  Infrastructure:
    cluster: colo
    jobNamespace: slipramp-runs
`,
			wantError:  "placement name",
			registered: true,
		},
		{
			name:      "invalid resource quantity",
			contents:  validPlacementsFile("    resources:\n      limits:\n        memory: many"),
			wantError: "invalid quantity",
		},
		{
			name: "unknown pod template field",
			contents: validPlacementsFile(`    podTemplate:
      spec:
        schedularName: custom`),
			wantError: "schedularName",
		},
		{
			name: "protected pod template field",
			contents: validPlacementsFile(`    podTemplate:
      spec:
        serviceAccountName: bypass`),
			wantError: "use serviceAccountName",
		},
		{
			name:      "null pod template",
			contents:  validPlacementsFile(`    podTemplate: null`),
			wantError: "must be an object",
		},
		{
			name:      "unregistered cluster",
			contents:  validPlacementsFile(""),
			wantError: "unregistered cluster",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			environment := map[string]string{
				serverPlacementsFileEnvironment: writePlacementsFile(
					t,
					test.contents,
				),
			}
			if test.registered {
				environment[serverControllerEnrollmentTokensEnvironment] =
					`{"colo":"enrollment-token"}`
			}

			_, err := config.LoadServer(mapEnvironment(environment))
			testcheck.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestLoadServerRejectsUnreadablePlacementsFile(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "missing.yaml")
	environment := map[string]string{
		serverPlacementsFileEnvironment: missingPath,
	}

	_, err := config.LoadServer(mapEnvironment(environment))
	testcheck.ErrorContains(t, err, serverPlacementsFileEnvironment)
}

func validPlacementsFile(extra string) string {
	return `
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  infrastructure:
    cluster: colo
    jobNamespace: slipramp-runs
` + extra + "\n"
}

func writePlacementsFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "placements.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return path
}
