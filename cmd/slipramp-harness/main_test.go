package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRequiresCommand(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	exitCode := run(nil, nil, nil, &stderr)

	if exitCode != 2 {
		t.Errorf("run() exit code = %d, want 2", exitCode)
	}

	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage", stderr.String())
	}
}
