package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []string
		want      string
	}{
		{name: "missing mode", want: "usage:"},
		{name: "invalid mode", arguments: []string{"plan", "site.yml"}, want: "usage:"},
		{name: "missing playbook", arguments: []string{"apply"}, want: "exactly one"},
		{
			name:      "multiple playbooks",
			arguments: []string{"preview", "site.yml", "other.yml"},
			want:      "exactly one",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			exitCode := run(context.Background(), test.arguments, &bytes.Buffer{}, &stderr, nil)
			if exitCode != 64 {
				t.Errorf("run() exit code = %d, want 64", exitCode)
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Errorf("run() stderr = %q, want %q", stderr.String(), test.want)
			}
		})
	}
}
