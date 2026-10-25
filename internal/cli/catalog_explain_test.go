package cli_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cli"
)

func TestCatalogExplainTextAndJSON(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	root := writeRepository(t, workDir, cliExplanationCatalog)
	placementsPath := filepath.Join(workDir, "placements.yaml")
	writeFile(t, placementsPath, validPlacements)
	for _, deployment := range []bool{false, true} {
		for _, format := range []string{"text", "json"} {
			arguments := []string{
				"catalog", "explain", "operations", "inspect", "--root", filepath.Base(root),
				"--format", format, "--set", "force=false", "--set", "version=a=b",
			}
			if deployment {
				arguments = append(arguments, "--placements", filepath.Base(placementsPath))
			}
			code, output, stderr := runApp(t, workDir, arguments...)
			if code != cli.ExitValid || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q, output = %s", code, stderr, output)
			}
			if strings.Contains(output, root) {
				t.Error("explanation contains absolute checkout path")
			}
			if format == "text" {
				for _, fragment := range []string{
					"operations", "inspect", "Base command", "Final argv: [\"inspect\"]", "a=b",
					"Runtime", "Timeout", "Parameter force: supplied: false", "Parameter optional: omitted",
					"Target operations default", ".slipramp/catalog.yaml document 2", "Target watch patterns",
				} {
					if !strings.Contains(output, fragment) {
						t.Errorf("text missing %q: %s", fragment, output)
					}
				}
				if !deployment && !strings.Contains(output, "deployment checks skipped") {
					t.Errorf("portable text = %s", output)
				}
				continue
			}
			var document catalog.Explanation
			if err := json.Unmarshal([]byte(output), &document); err != nil {
				t.Fatal(err)
			}
			if document.SchemaVersion != 1 || document.Target.Value.Name != "operations" ||
				document.Action.Value != "inspect" || document.Placement.ConfigurationAvailable != deployment ||
				document.Process.Environment["VERSION"].Value != "a=b" ||
				!reflect.DeepEqual(document.Process.Argv.Value, []string{"inspect"}) ||
				document.Runtime.Name.Provenance[0].Kind != "target-default" {
				t.Errorf("JSON explanation = %#v", document)
			}
			_, repeated, _ := runApp(t, workDir, arguments...)
			if repeated != output {
				t.Error("JSON output is not deterministic")
			}
		}
	}
}

func TestCatalogExplainDefaultsRootAndAcceptsLeadingFlags(t *testing.T) {
	t.Parallel()

	root := writeRepository(t, t.TempDir(), validCatalog)
	for _, args := range [][]string{
		{"catalog", "explain", "operations", "inspect"},
		{"catalog", "explain", "--format", "json", "operations", "inspect"},
	} {
		code, output, stderr := runApp(t, root, args...)
		if code != cli.ExitValid || output == "" || stderr != "" {
			t.Fatalf("exit = %d, output = %s, stderr = %s", code, output, stderr)
		}
	}
}

func TestCatalogExplainFailures(t *testing.T) {
	t.Parallel()

	root := writeRepository(t, t.TempDir(), cliExplanationCatalog)
	tests := []struct {
		name    string
		args    []string
		code    int
		message string
	}{
		{"missing action", []string{"operations"}, cli.ExitInvocation, "requires TARGET ACTION"},
		{"extra argument", []string{"operations", "inspect", "extra"}, cli.ExitInvocation, "requires TARGET ACTION"},
		{"format", []string{"operations", "inspect", "--format", "sarif"}, cli.ExitInvocation, "unsupported output format"},
		{"flag", []string{"operations", "inspect", "--connected"}, cli.ExitInvocation, "flag provided but not defined"},
		{"assignment", []string{"operations", "inspect", "--set", "no-equals"}, cli.ExitInvocation, "NAME=VALUE"},
		{
			"duplicate", []string{"operations", "inspect", "--set", "version=a", "--set", "version=b"},
			cli.ExitInvocation, "more than once",
		},
		{"root", []string{"operations", "inspect", "--root", "missing"}, cli.ExitInvocation, "inspect repository root"},
		{
			"placement file", []string{"operations", "inspect", "--placements", "missing"},
			cli.ExitInvocation, "read placement configuration",
		},
		{"target", []string{"absent", "inspect"}, cli.ExitInvalid, "unknown Target"},
		{"action", []string{"operations", "absent"}, cli.ExitInvalid, "unknown Action"},
		{"required", []string{"operations", "inspect"}, cli.ExitInvalid, "was not supplied"},
		{"unknown parameter", []string{"operations", "inspect", "--set", "unknown=true"}, cli.ExitInvalid, "not declared"},
		{"bad boolean", []string{"operations", "inspect", "--set", "force=maybe"}, cli.ExitInvalid, "must be a boolean"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			arguments := append([]string{"catalog", "explain"}, test.args...)
			code, stdout, stderr := runApp(t, root, arguments...)
			if code != test.code || stdout != "" || !strings.Contains(stderr, test.message) {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q; want %d, %q", code, stdout, stderr, test.code, test.message)
			}
		})
	}
	code, output, stderr := runApp(t, root, "catalog", "explain", "--help")
	if code != cli.ExitValid || stderr != "" || !strings.Contains(output, "TARGET ACTION [flags]") {
		t.Fatalf("help exit = %d, output = %q, stderr = %q", code, output, stderr)
	}
}

func TestCatalogExplainRejectsWholeInvalidCatalog(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"text", "json"} {
		root := writeRepository(t, t.TempDir(), validCatalog)
		// A selected valid Action cannot bypass an unrelated invalid document.
		writeFile(t, filepath.Join(root, ".slipramp", "other.yaml"),
			strings.Replace(validCatalog, "name: tools", "name: other-tools", 1))
		code, stdout, stderr := runApp(t, root, "catalog", "explain", "operations", "inspect", "--format", format)
		if code != cli.ExitInvalid || stderr != "" || !strings.Contains(stdout, "catalog.resource.duplicate") {
			t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
	}
}

func TestCatalogExplainDeploymentValidation(t *testing.T) {
	t.Parallel()

	for name, contents := range map[string]string{
		"missing Placement":   strings.Replace(validPlacements, "  test:", "  other:", 1),
		"empty Placement set": "apiVersion: slipramp.com/v1alpha1\nkind: PlacementConfig\nplacements: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := writeRepository(t, t.TempDir(), validCatalog)
			writeFile(t, filepath.Join(root, "placements.yaml"), contents)
			code, output, stderr := runApp(t, root,
				"catalog", "explain", "operations", "inspect", "--placements", "placements.yaml")
			if code != cli.ExitInvalid || stderr != "" || !strings.Contains(output, "catalog.placement.not-found") {
				t.Fatalf("exit = %d, output = %q, stderr = %q", code, output, stderr)
			}
		})
	}
}

const cliExplanationCatalog = `apiVersion: slipramp.com/v1alpha1
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
      parameters:
        force: {type: boolean, argument: {name: --force}}
        optional: {type: string, argument: {}}
        version: {type: string, required: true, environment: VERSION}
`
