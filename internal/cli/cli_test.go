package cli_test

import (
	"bytes"
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

func TestVersion(t *testing.T) {
	t.Parallel()

	code, output, stderr := runApp(t, t.TempDir(), "version")
	if code != cli.ExitValid || stderr != "" ||
		output != "slipramp test-version\nsupported catalog API versions: slipramp.com/v1alpha1\n" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}

func runApp(t *testing.T, workDir string, arguments ...string) (int, string, string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.App{
		Stdout:  &stdout,
		Stderr:  &stderr,
		WorkDir: workDir,
		Version: "test-version",
	}.Run(arguments)

	return code, stdout.String(), stderr.String()
}

func writeRepository(t *testing.T, workDir, contents string) string {
	t.Helper()

	root := filepath.Join(workDir, "repository")
	if err := os.MkdirAll(filepath.Join(root, ".slipramp"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, filepath.Join(root, ".slipramp", "catalog.yaml"), contents)

	return root
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

const validCatalog = `
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
  runtime: tools
  placement: test
  actions:
    inspect:
      command: [inspect]
`

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
      command: [inspect]
`

const validPlacements = `
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  test:
    cluster: test
    jobNamespace: slipramp-runs
`
