package cli_test

import (
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestCommandTreeHelp(t *testing.T) {
	t.Parallel()

	code, output, stderr := runApp(t, t.TempDir(), "--help")
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("root help exit = %d, stderr = %q", code, stderr)
	}
	for _, command := range []string{"artifact", "catalog", "run", "target", "version"} {
		if !strings.Contains(output, command) {
			t.Errorf("root help missing %q: %s", command, output)
		}
	}
	if strings.Contains(output, "completion") {
		t.Errorf("root help advertises disabled completion command: %s", output)
	}

	code, output, stderr = runApp(t, t.TempDir(), "run", "create", "--help")
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("Run creation help exit = %d, stderr = %q", code, stderr)
	}
	for _, option := range []string{"--action", "--idempotency-key", "--input", "--server", "--set", "--target"} {
		if !strings.Contains(output, option) {
			t.Errorf("Run creation help missing %q: %s", option, output)
		}
	}
}

func TestCommandTreeInvocationFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{name: "missing command", message: "a command is required"},
		{name: "unknown command", args: []string{"launch"}, message: `unknown command "launch"`},
		{name: "missing Run command", args: []string{"run"}, message: "run requires"},
		{name: "missing Run ID", args: []string{"run", "get"}, message: "exactly one Run ID is required"},
		{name: "missing Target command", args: []string{"target"}, message: "target requires"},
		{name: "missing Target ID", args: []string{"target", "get"}, message: "exactly one Target ID is required"},
		{name: "missing Target Action", args: []string{"target", "explain", testTargetID}, message: "requires TARGET_ID ACTION"},
		{name: "missing Artifact command", args: []string{"artifact"}, message: "artifact requires"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			code, output, stderr := runApp(t, t.TempDir(), test.args...)
			if code != cli.ExitInvocation || output != "" || !strings.Contains(stderr, test.message) {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
			}
		})
	}
}
