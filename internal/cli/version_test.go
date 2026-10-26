package cli_test

import (
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestVersion(t *testing.T) {
	t.Parallel()

	code, output, stderr := runApp(t, t.TempDir(), "version")
	if code != cli.ExitValid || stderr != "" ||
		output != "slipramp test-version\nsupported catalog API versions: slipramp.com/v1alpha1\n" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}
