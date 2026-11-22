package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestCatalogValidatePortableAndDeployment(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	repositoryRoot := writeRepository(t, workDir, validCatalog)
	placementsPath := filepath.Join(workDir, "placements.yaml")
	writeFile(t, placementsPath, validPlacements)

	portableCode, portableOutput, portableError := runApp(
		t,
		workDir,
		"catalog",
		"validate",
		filepath.Base(repositoryRoot),
	)
	if portableCode != cli.ExitValid || portableError != "" {
		t.Fatalf("portable exit = %d, stderr = %q", portableCode, portableError)
	}
	if !strings.Contains(portableOutput, "Catalog is portable-valid") ||
		!strings.Contains(portableOutput, "Deployment checks were not run for placements: test") {
		t.Errorf("portable output = %q", portableOutput)
	}

	deploymentCode, deploymentOutput, deploymentError := runApp(
		t,
		workDir,
		"catalog",
		"validate",
		"--placements",
		filepath.Base(placementsPath),
		filepath.Base(repositoryRoot),
	)
	if deploymentCode != cli.ExitValid || deploymentError != "" {
		t.Fatalf("deployment exit = %d, stderr = %q", deploymentCode, deploymentError)
	}
	if !strings.Contains(deploymentOutput, "Catalog is valid against 1 placements") {
		t.Errorf("deployment output = %q", deploymentOutput)
	}
}

func TestCatalogValidateStructuredOutputAndStrictWarnings(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	repositoryRoot := writeRepository(t, workDir, warningCatalog)

	code, output, stderr := runApp(
		t,
		workDir,
		"catalog",
		"validate",
		"--format",
		"json",
		"--strict",
		filepath.Base(repositoryRoot),
	)
	if code != cli.ExitInvalid || stderr != "" {
		t.Fatalf("JSON exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(output, `"schemaVersion": 1`) ||
		!strings.Contains(output, `"valid": false`) ||
		!strings.Contains(output, `"code": "catalog.target.path-not-found"`) {
		t.Errorf("JSON output = %q", output)
	}
	if strings.Contains(output, workDir) {
		t.Errorf("JSON output contains absolute checkout path: %q", output)
	}

	code, output, stderr = runApp(
		t,
		workDir,
		"catalog",
		"validate",
		"--format",
		"sarif",
		filepath.Base(repositoryRoot),
	)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("SARIF exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(output, `"version": "2.1.0"`) ||
		!strings.Contains(output, `"name": "slipramp/catalog"`) {
		t.Errorf("SARIF output = %q", output)
	}
	if strings.Contains(output, workDir) {
		t.Errorf("SARIF output contains absolute checkout path: %q", output)
	}
}

func TestCatalogValidateGateSources(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	repositoryRoot := writeRepository(t, workDir, validCatalog+`
---
apiVersion: slipramp.com/v1alpha1
kind: Gate
metadata:
  name: maintenance-safe
spec:
  evaluator:
    type: prometheus
    source: homelab
    query: maintenance_active
    condition: {operator: equal, value: 0}
`)
	configuredPath := filepath.Join(workDir, "gate-sources.yaml")
	writeFile(t, configuredPath, validGateSources)

	code, output, stderr := runApp(
		t,
		workDir,
		"catalog",
		"validate",
		"--gate-sources",
		filepath.Base(configuredPath),
		filepath.Base(repositoryRoot),
	)
	if code != cli.ExitValid || stderr != "" || !strings.Contains(output, "Catalog is valid") {
		t.Fatalf("configured exit = %d, output = %q, stderr = %q", code, output, stderr)
	}

	writeFile(t, configuredPath, strings.Replace(validGateSources, "homelab:", "other:", 1))
	code, output, stderr = runApp(
		t,
		workDir,
		"catalog",
		"validate",
		"--gate-sources",
		filepath.Base(configuredPath),
		filepath.Base(repositoryRoot),
	)
	if code != cli.ExitInvalid || stderr != "" ||
		!strings.Contains(output, "catalog.gate.source-not-found") {
		t.Fatalf("missing source exit = %d, output = %q, stderr = %q", code, output, stderr)
	}
}

func TestCatalogValidateWebhookSourceType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	repo := writeRepository(t, dir, validCatalog+`
---
apiVersion: slipramp.com/v1alpha1
kind: Gate
metadata: {name: approval}
spec:
  evaluator: {type: webhook, source: homelab, input: {policy: deploy}}
`)
	for _, tc := range []struct {
		sourceType string
		want       int
	}{
		{"webhook", cli.ExitValid}, {"prometheus", cli.ExitInvalid},
	} {
		writeFile(t, filepath.Join(dir, "sources.yaml"), strings.Replace(validGateSources, "type: prometheus", "type: "+tc.sourceType, 1))
		code, output, stderr := runApp(t, dir, "catalog", "validate", "--gate-sources", "sources.yaml", filepath.Base(repo))
		if code != tc.want || stderr != "" || (code == cli.ExitInvalid && !strings.Contains(output, "catalog.gate.source-type-mismatch")) {
			t.Fatalf("%s source: exit %d, output %q, stderr %q", tc.sourceType, code, output, stderr)
		}
	}
}

func TestCatalogValidateGoldenOutput(t *testing.T) {
	t.Parallel()

	workDir := filepath.Join("testdata", "catalog-validate")
	for _, format := range []string{"text", "json", "sarif"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			code, output, stderr := runApp(
				t,
				workDir,
				"catalog",
				"validate",
				"--format",
				format,
				"--strict",
			)
			if code != cli.ExitInvalid || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}

			goldenPath := filepath.Join(workDir, format+".golden")
			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("os.ReadFile(%q) error = %v", goldenPath, err)
			}
			if output != string(expected) {
				t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, output, expected)
			}
		})
	}
}

func TestCatalogValidateInvocationFailures(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	code, _, stderr := runApp(t, workDir, "catalog", "validate", "--format", "xml")
	if code != cli.ExitInvocation || !strings.Contains(stderr, "unsupported output format") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

const warningCatalog = `
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: tools
spec:
  image: example.com/tools:1
---
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: operations
spec:
  path: missing
  runtime: tools
  placement: test
  actions:
    inspect:
      intent: preview
      command: [inspect]
`

const validGateSources = `
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  homelab:
    type: prometheus
    url: http://prometheus:9090
`
