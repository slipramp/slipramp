package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

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

const validPlacements = `
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  test:
    cluster: test
    jobNamespace: slipramp-runs
`
