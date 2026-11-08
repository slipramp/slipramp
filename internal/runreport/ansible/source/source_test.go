package source

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageSource(t *testing.T) {
	t.Parallel()

	runnerArtifacts := filepath.Join(t.TempDir(), "runner")
	eventDirectory := filepath.Join(runnerArtifacts, "job_events")
	if err := os.MkdirAll(eventDirectory, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	writeTestEvent(t, eventDirectory, "2-event-2.json", 2, "playbook_on_stats")
	writeTestEvent(t, eventDirectory, "1-event-1.json", 1, "playbook_on_start")
	if err := os.WriteFile(filepath.Join(eventDirectory, "event-partial.json"), []byte(`{"counter":3}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(runnerArtifacts, "status"), []byte("failed"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(runnerArtifacts, "rc"), []byte("23"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	destination := filepath.Join(t.TempDir(), "reports", "ansible-playbook.json")
	err := PackageSource(PackageConfig{
		Destination:             destination,
		Mode:                    ModePreview,
		RunnerArtifactDirectory: runnerArtifacts,
		RepositoryDirectory:     "/workspace",
		RunnerExitCodeFallback:  1,
		Versions: Versions{
			AnsibleCore:   "2.21.5",
			AnsibleRunner: "2.4.3",
		},
	})
	if err != nil {
		t.Fatalf("PackageSource() error = %v", err)
	}

	source := readSource(t, destination)
	if source.SchemaVersion != SourceSchemaVersion || source.Mode != ModePreview || source.RepositoryDirectory != "/workspace" {
		t.Errorf("source envelope = version %d mode %q", source.SchemaVersion, source.Mode)
	}
	if source.Versions.AnsibleCore != "2.21.5" || source.Versions.AnsibleRunner != "2.4.3" {
		t.Errorf("source versions = %#v", source.Versions)
	}
	if source.Runner.Status != RunnerStatusFailed || source.Runner.ExitCode != 23 {
		t.Errorf("source Runner result = %#v", source.Runner)
	}
	if len(source.Events) != 2 || eventCounter(t, source.Events[0]) != 1 || eventCounter(t, source.Events[1]) != 2 {
		t.Errorf("source events = %s", source.Events)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("report permissions = %o, want no group or other access", info.Mode().Perm())
	}
}

func TestPackageSourceUsesPartialFallback(t *testing.T) {
	t.Parallel()

	runnerArtifacts := t.TempDir()
	destination := filepath.Join(t.TempDir(), "ansible-playbook.json")
	err := PackageSource(PackageConfig{
		Destination:             destination,
		Mode:                    ModeApply,
		RunnerArtifactDirectory: runnerArtifacts,
		RepositoryDirectory:     "/workspace",
		RunnerExitCodeFallback:  143,
		Canceled:                true,
	})
	if err != nil {
		t.Fatalf("PackageSource() error = %v", err)
	}

	source := readSource(t, destination)
	if source.Runner.Status != RunnerStatusCanceled || source.Runner.ExitCode != 143 {
		t.Errorf("source Runner result = %#v", source.Runner)
	}
	if source.Events == nil || len(source.Events) != 0 {
		t.Errorf("source events = %#v, want an empty array", source.Events)
	}
}

func TestPackageSourceRejectsInvalidEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string
		wantError string
	}{
		{
			name: "duplicate counter",
			files: map[string]string{
				"1-event-a.json": `{"counter":1}`,
				"1-event-b.json": `{"counter":1}`,
			},
			wantError: "event counter 1 is duplicated",
		},
		{
			name:      "counter mismatch",
			files:     map[string]string{"1-event.json": `{"counter":2}`},
			wantError: "event counter does not match",
		},
		{
			name:      "invalid filename",
			files:     map[string]string{"event.json": `{"counter":1}`},
			wantError: "event filename is invalid",
		},
		{
			name:      "empty event identifier",
			files:     map[string]string{"1-.json": `{"counter":1}`},
			wantError: "event filename is invalid",
		},
		{
			name:      "invalid JSON",
			files:     map[string]string{"1-event.json": `{`},
			wantError: "not valid UTF-8 JSON",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runnerArtifacts := filepath.Join(t.TempDir(), "runner")
			eventDirectory := filepath.Join(runnerArtifacts, "job_events")
			if err := os.MkdirAll(eventDirectory, 0o755); err != nil {
				t.Fatalf("os.MkdirAll() error = %v", err)
			}
			for name, contents := range test.files {
				if err := os.WriteFile(filepath.Join(eventDirectory, name), []byte(contents), 0o600); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}

			destination := filepath.Join(t.TempDir(), "ansible-playbook.json")
			err := PackageSource(PackageConfig{
				Destination:             destination,
				Mode:                    ModeApply,
				RunnerArtifactDirectory: runnerArtifacts,
				RepositoryDirectory:     "/workspace",
			})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("PackageSource() error = %v, want %q", err, test.wantError)
			}
			if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("report path error = %v, want not found", err)
			}
		})
	}
}

func writeTestEvent(t *testing.T, directory, name string, counter int, event string) {
	t.Helper()

	contents, err := json.Marshal(map[string]any{
		"counter": counter,
		"uuid":    name,
		"event":   event,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), contents, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

func eventCounter(t *testing.T, contents json.RawMessage) int {
	t.Helper()

	var event struct {
		Counter int `json:"counter"`
	}
	if err := json.Unmarshal(contents, &event); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	return event.Counter
}
