package source

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	project := testProject(t)
	runner, playbook := testBinaries(t)

	tests := []struct {
		name        string
		mode        Mode
		limit       string
		inventory   string
		wantCmdline string
	}{
		{name: "preview", mode: ModePreview, limit: "node01.example.com", wantCmdline: "--check --diff"},
		{name: "apply", mode: ModeApply, inventory: filepath.Join(project, "inventory", "cloud.yml")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			temporary := t.TempDir()
			recordPath := filepath.Join(temporary, "runner-arguments.json")
			reportPath := filepath.Join(temporary, "ansible-playbook.json")
			t.Setenv("FAKE_ANSIBLE_RECORD", recordPath)
			t.Setenv("FAKE_ANSIBLE_RC", "23")

			var stdout, stderr bytes.Buffer
			exitCode, err := Run(t.Context(), Config{
				Mode:             test.mode,
				ProjectDirectory: project,
				Playbook:         "site.yml",
				Limit:            test.limit,
				Inventory:        test.inventory,
				ReportPath:       reportPath,
				RunnerBinary:     runner,
				PlaybookBinary:   playbook,
				Stdout:           &stdout,
				Stderr:           &stderr,
			})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if exitCode != 23 {
				t.Errorf("Run() exit code = %d, want 23", exitCode)
			}
			if !strings.Contains(stdout.String(), "ansible stdout") ||
				!strings.Contains(stderr.String(), "ansible stderr") {
				t.Errorf("Run() output = stdout %q stderr %q", stdout.String(), stderr.String())
			}

			contents, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			var arguments []string
			if err := json.Unmarshal(contents, &arguments); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			assertArgument(t, arguments, "--project-dir", project)
			wantInventory := test.inventory
			if wantInventory == "" {
				wantInventory = filepath.Join(project, "inventory", "hosts.yml")
			}
			assertArgument(t, arguments, "--inventory", wantInventory)
			assertArgument(t, arguments, "--playbook", "site.yml")
			assertArgument(t, arguments, "--ident", defaultRunnerIdentity)
			if !slices.Contains(arguments, "--omit-env-files") {
				t.Errorf("Runner arguments = %#v, want --omit-env-files", arguments)
			}
			if test.wantCmdline != "" {
				assertArgument(t, arguments, "--cmdline", test.wantCmdline)
			} else if slices.Contains(arguments, "--cmdline") {
				t.Errorf("apply Runner arguments unexpectedly include --cmdline: %#v", arguments)
			}
			if test.limit != "" {
				assertArgument(t, arguments, "--limit", test.limit)
			} else if slices.Contains(arguments, "--limit") {
				t.Errorf("apply Runner arguments unexpectedly include --limit: %#v", arguments)
			}

			source := readSource(t, reportPath)
			if source.SchemaVersion != SourceSchemaVersion || source.Mode != test.mode ||
				source.RepositoryDirectory != filepath.Dir(project) {
				t.Errorf("source envelope = version %d mode %q", source.SchemaVersion, source.Mode)
			}
			if source.Versions.AnsibleCore != "2.21.5" || source.Versions.AnsibleRunner != "2.4.3" {
				t.Errorf("source versions = %#v", source.Versions)
			}
			if source.Runner.Status != RunnerStatusFailed || source.Runner.ExitCode != 23 {
				t.Errorf("source Runner result = %#v", source.Runner)
			}
			if len(source.Events) != 2 || eventCounter(t, source.Events[0]) != 1 ||
				eventCounter(t, source.Events[1]) != 2 {
				t.Errorf("source events = %s", source.Events)
			}
		})
	}
}

func TestRunRejectsPlaybookOutsideProject(t *testing.T) {
	project := testProject(t)
	if err := os.WriteFile(filepath.Join(project, "..", "site.yml"), []byte("---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	exitCode, err := Run(t.Context(), Config{
		Mode:             ModePreview,
		ProjectDirectory: project,
		Playbook:         "../site.yml",
	})
	if exitCode != 64 || err == nil || !strings.Contains(err.Error(), "stay inside the project directory") {
		t.Fatalf("Run() = exit %d, error %v", exitCode, err)
	}
}

func TestRunPreservesRunnerFailureWhenPackagingFails(t *testing.T) {
	project := testProject(t)
	runner, playbook := testBinaries(t)

	tests := []struct {
		name     string
		runnerRC string
		wantRC   int
	}{
		{name: "successful playbook", runnerRC: "0", wantRC: 70},
		{name: "failed playbook", runnerRC: "23", wantRC: 23},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("FAKE_ANSIBLE_RC", test.runnerRC)
			exitCode, err := Run(t.Context(), Config{
				Mode:             ModeApply,
				ProjectDirectory: project,
				Playbook:         "site.yml",
				RunnerBinary:     runner,
				PlaybookBinary:   playbook,
				ReportPath:       t.TempDir(),
			})
			if exitCode != test.wantRC || err == nil || !strings.Contains(err.Error(), "package Ansible report") {
				t.Fatalf("Run() = exit %d, error %v", exitCode, err)
			}
		})
	}
}

func TestRunForwardsTermination(t *testing.T) {
	project := testProject(t)
	runner, playbook := testBinaries(t)
	temporary := t.TempDir()
	readyPath := filepath.Join(temporary, "ready")
	reportPath := filepath.Join(temporary, "report.json")
	t.Setenv("FAKE_ANSIBLE_READY", readyPath)
	signals := make(chan os.Signal, 1)

	type runResult struct {
		exitCode int
		err      error
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan runResult, 1)
	go func() {
		exitCode, err := Run(ctx, Config{
			Mode:             ModeApply,
			ProjectDirectory: project,
			Playbook:         "site.yml",
			RunnerBinary:     runner,
			PlaybookBinary:   playbook,
			ReportPath:       reportPath,
			Signals:          signals,
		})
		result <- runResult{exitCode: exitCode, err: err}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			select {
			case <-result:
			case <-time.After(5 * time.Second):
			}
			t.Fatal("Ansible Runner did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	signals <- syscall.SIGTERM

	select {
	case finished := <-result:
		if finished.exitCode != 143 || finished.err != nil {
			t.Fatalf("Run() = exit %d, error %v", finished.exitCode, finished.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not finish after termination")
	}

	source := readSource(t, reportPath)
	if source.Runner.Status != RunnerStatusCanceled || source.Runner.ExitCode != 254 {
		t.Errorf("source Runner result = %#v", source.Runner)
	}
	if len(source.Events) != 1 || eventCounter(t, source.Events[0]) != 1 {
		t.Errorf("partial source events = %s", source.Events)
	}
}

func testProject(t *testing.T) string {
	t.Helper()

	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	project = filepath.Join(project, "ansible")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "site.yml"), []byte("---\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, "inventory"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, "inventory", "hosts.yml"), []byte("all: {}\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	return project
}

func testBinaries(t *testing.T) (string, string) {
	t.Helper()

	runner, err := filepath.Abs(filepath.Join("testdata", "ansible-runner"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	playbook, err := filepath.Abs(filepath.Join("testdata", "ansible-playbook"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}

	return runner, playbook
}

func readSource(t *testing.T, path string) Document {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	var source Document
	if err := json.Unmarshal(contents, &source); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	return source
}

func assertArgument(t *testing.T, arguments []string, name, want string) {
	t.Helper()

	index := slices.Index(arguments, name)
	if index < 0 || index+1 >= len(arguments) {
		t.Fatalf("Runner arguments = %#v, want %s %q", arguments, name, want)
	}
	if arguments[index+1] != want {
		t.Errorf("Runner argument %s = %q, want %q", name, arguments[index+1], want)
	}
}
