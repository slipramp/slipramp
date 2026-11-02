package ansiblereport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLaunch(t *testing.T) {
	project := testProject(t)
	runner, playbook := testBinaries(t)

	tests := []struct {
		name        string
		mode        Mode
		limit       string
		wantCmdline string
	}{
		{name: "preview", mode: ModePreview, limit: "node01.example.com", wantCmdline: "--check --diff"},
		{name: "apply", mode: ModeApply},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			temporary := t.TempDir()
			recordPath := filepath.Join(temporary, "runner-arguments.json")
			reportPath := filepath.Join(temporary, "ansible-playbook.json")
			t.Setenv("FAKE_ANSIBLE_RECORD", recordPath)
			t.Setenv("FAKE_ANSIBLE_RC", "23")

			var stdout, stderr bytes.Buffer
			exitCode, err := Launch(t.Context(), Config{
				Mode:             test.mode,
				ProjectDirectory: project,
				Playbook:         "site.yml",
				Limit:            test.limit,
				ReportPath:       reportPath,
				RunnerBinary:     runner,
				PlaybookBinary:   playbook,
				Stdout:           &stdout,
				Stderr:           &stderr,
			})
			if err != nil {
				t.Fatalf("Launch() error = %v", err)
			}
			if exitCode != 23 {
				t.Errorf("Launch() exit code = %d, want 23", exitCode)
			}
			if !strings.Contains(stdout.String(), "ansible stdout") ||
				!strings.Contains(stderr.String(), "ansible stderr") {
				t.Errorf("Launch() output = stdout %q stderr %q", stdout.String(), stderr.String())
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
			assertArgument(t, arguments, "--inventory", filepath.Join(project, "inventory", "hosts.yml"))
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
			if source.SchemaVersion != SourceSchemaVersion || source.Mode != test.mode {
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

func TestLaunchValidatesPlaybook(t *testing.T) {
	project := testProject(t)
	runner, playbook := testBinaries(t)

	exitCode, err := Launch(t.Context(), Config{
		Mode:             ModePreview,
		ProjectDirectory: project,
		Playbook:         "../site.yml",
		RunnerBinary:     runner,
		PlaybookBinary:   playbook,
		ReportPath:       filepath.Join(t.TempDir(), "report.json"),
	})
	if exitCode != 64 || err == nil || !strings.Contains(err.Error(), "resolve playbook") {
		t.Fatalf("Launch() = exit %d, error %v", exitCode, err)
	}
}

func TestLaunchPreservesRunnerFailureWhenPackagingFails(t *testing.T) {
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
			exitCode, err := Launch(t.Context(), Config{
				Mode:             ModeApply,
				ProjectDirectory: project,
				Playbook:         "site.yml",
				RunnerBinary:     runner,
				PlaybookBinary:   playbook,
				ReportPath:       t.TempDir(),
			})
			if exitCode != test.wantRC || err == nil || !strings.Contains(err.Error(), "package Ansible report") {
				t.Fatalf("Launch() = exit %d, error %v", exitCode, err)
			}
		})
	}
}

func TestLaunchRejectsDuplicateCounters(t *testing.T) {
	project := testProject(t)
	runner, playbook := testBinaries(t)
	t.Setenv("FAKE_ANSIBLE_DUPLICATE_COUNTER", "1")
	reportPath := filepath.Join(t.TempDir(), "report.json")

	exitCode, err := Launch(t.Context(), Config{
		Mode:             ModeApply,
		ProjectDirectory: project,
		Playbook:         "site.yml",
		RunnerBinary:     runner,
		PlaybookBinary:   playbook,
		ReportPath:       reportPath,
	})
	if exitCode != 70 || err == nil || !strings.Contains(err.Error(), "event counter 1 is duplicated") {
		t.Fatalf("Launch() = exit %d, error %v", exitCode, err)
	}
	if _, err := os.Stat(reportPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("report path error = %v, want not found", err)
	}
}

func TestLaunchForwardsTermination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group signal behavior is POSIX-specific")
	}

	project := testProject(t)
	runner, playbook := testBinaries(t)
	temporary := t.TempDir()
	readyPath := filepath.Join(temporary, "ready")
	reportPath := filepath.Join(temporary, "report.json")
	t.Setenv("FAKE_ANSIBLE_READY", readyPath)
	signals := make(chan os.Signal, 1)

	type launchResult struct {
		exitCode int
		err      error
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan launchResult, 1)
	go func() {
		exitCode, err := Launch(ctx, Config{
			Mode:             ModeApply,
			ProjectDirectory: project,
			Playbook:         "site.yml",
			RunnerBinary:     runner,
			PlaybookBinary:   playbook,
			ReportPath:       reportPath,
			Signals:          signals,
		})
		result <- launchResult{exitCode: exitCode, err: err}
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
			t.Fatalf("Launch() = exit %d, error %v", finished.exitCode, finished.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Launch() did not finish after termination")
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

func readSource(t *testing.T, path string) Source {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	var source Source
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

func TestResolveConfigUsesExplicitInventory(t *testing.T) {
	t.Parallel()

	project := testProject(t)
	inventory := filepath.Join(t.TempDir(), "inventory.py")
	if err := os.WriteFile(inventory, []byte("#!/usr/bin/env python3\n"), 0o700); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	resolved, err := resolveConfig(Config{
		Mode:             ModeApply,
		ProjectDirectory: project,
		Inventory:        inventory,
		Playbook:         "site.yml",
	})
	if err != nil {
		t.Fatalf("resolveConfig() error = %v", err)
	}
	if resolved.Inventory != inventory {
		t.Errorf("resolved inventory = %q, want %q", resolved.Inventory, inventory)
	}
}
