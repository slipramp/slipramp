package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

func TestInfrastructureOpsExample(t *testing.T) {
	t.Parallel()

	exampleRoot := filepath.Join("..", "..", "examples", "infrastructure-ops")
	portable, err := catalog.Process(os.DirFS(exampleRoot), catalog.Options{})
	if err != nil {
		t.Fatalf("portable catalog.Process() error = %v", err)
	}
	if portable.Level != catalog.ValidationLevelPortable {
		t.Errorf("portable level = %q, want %q", portable.Level, catalog.ValidationLevelPortable)
	}
	if portable.Diagnostics.HasErrors() {
		t.Fatalf("portable catalog.Process() diagnostics = %s", portable.Diagnostics.ErrorSummary())
	}

	placements, err := placement.LoadFile(filepath.Join(exampleRoot, "placements.yaml"))
	if err != nil {
		t.Fatalf("placement.LoadFile() error = %v", err)
	}

	result, err := catalog.Process(os.DirFS(exampleRoot), catalog.Options{Placements: &placements})
	if err != nil {
		t.Fatalf("deployment catalog.Process() error = %v", err)
	}
	if result.Level != catalog.ValidationLevelDeployment {
		t.Errorf("deployment level = %q, want %q", result.Level, catalog.ValidationLevelDeployment)
	}
	if result.Diagnostics.HasErrors() {
		t.Fatalf("deployment catalog.Process() diagnostics = %s", result.Diagnostics.ErrorSummary())
	}
	loaded := result.Catalog

	var ansible, rustfs *catalog.Target
	for _, target := range loaded.Targets {
		switch target.Metadata.Name {
		case "ansible-baseline":
			ansible = &target
		case "rustfs-buckets":
			rustfs = &target
		}
	}
	if ansible == nil {
		t.Fatal("ansible-baseline target not found")
	}
	if ansible.Spec.Credentials["ssh"] != "ansible-ssh" ||
		ansible.Spec.Credentials["ssh-known-hosts"] != "ssh-known-hosts" {
		t.Errorf("ansible credentials = %#v", ansible.Spec.Credentials)
	}
	direct, found := ansible.EffectiveAction("direct-playbook")
	if !found {
		t.Fatal("ansible direct-playbook action not found")
	}
	wantDirectCommand := []string{"ansible-playbook", "--inventory", "inventory/hosts.yml", "site.yml"}
	if !reflect.DeepEqual(direct.Command, wantDirectCommand) {
		t.Errorf("ansible direct-playbook command = %#v, want %#v", direct.Command, wantDirectCommand)
	}
	if len(direct.Artifacts) != 0 {
		t.Errorf("ansible direct-playbook artifacts = %#v, want none", direct.Artifacts)
	}
	for name, action := range ansible.Spec.Actions {
		if len(action.Artifacts) != 0 {
			t.Errorf("ansible action %q advertises artifacts before the report contract: %#v", name, action.Artifacts)
		}
	}

	if rustfs == nil {
		t.Fatal("rustfs-buckets target not found")
	}
	apply, found := rustfs.EffectiveAction("apply")
	if !found {
		t.Fatal("rustfs apply action not found")
	}
	plan := apply.Inputs["plan"]
	if plan.Artifact.Compatibility != catalog.ArtifactCompatibilityExactCatalogRevision {
		t.Errorf(
			"plan compatibility = %q, want %q",
			plan.Artifact.Compatibility,
			catalog.ArtifactCompatibilityExactCatalogRevision,
		)
	}

	syncAction, found := rustfs.EffectiveAction("sync")
	if !found {
		t.Fatal("rustfs sync action not found")
	}
	if len(syncAction.Triggers) != 1 || syncAction.Triggers[0].Review != catalog.TriggerReviewRequired {
		t.Errorf("rustfs sync triggers = %#v, want one review-required trigger", syncAction.Triggers)
	}
}

func TestInfrastructureOpsAnsibleLauncher(t *testing.T) {
	t.Parallel()

	python, err := python3Executable(t.Context())
	if err != nil {
		t.Skip("python3 is required to test the example Ansible launcher")
	}

	exampleRoot := filepath.Join("..", "..", "examples", "infrastructure-ops", "ansible")
	launcher := filepath.Join("slipramp", "ansible.py")
	executableRoot, err := filepath.Abs(filepath.Join("testdata", "ansible-launcher"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}

	tests := []struct {
		name      string
		arguments []string
		wantTail  []string
	}{
		{
			name:      "preview",
			arguments: []string{"preview", "site.yml", "--limit", "node01.example.com"},
			wantTail:  []string{"site.yml", "--check", "--diff", "--limit", "node01.example.com"},
		},
		{name: "apply", arguments: []string{"apply", "site.yml"}, wantTail: []string{"site.yml"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recordPath := filepath.Join(t.TempDir(), "ansible.json")
			command := exec.CommandContext(t.Context(), python, append([]string{launcher}, test.arguments...)...)
			command.Dir = exampleRoot
			command.Env = append(
				environmentWithPath(executableRoot),
				"FAKE_ANSIBLE_RECORD="+recordPath,
				"FAKE_ANSIBLE_RC=23",
			)
			output, runError := command.CombinedOutput()
			var exitError *exec.ExitError
			if !errors.As(runError, &exitError) || exitError.ExitCode() != 23 {
				t.Fatalf("launcher error = %v, output = %s", runError, output)
			}
			if !strings.Contains(string(output), "ansible stdout") ||
				!strings.Contains(string(output), "ansible stderr") {
				t.Errorf("launcher output = %q", output)
			}

			contents, readError := os.ReadFile(recordPath)
			if readError != nil {
				t.Fatalf("os.ReadFile() error = %v", readError)
			}
			var record []string
			if decodeError := json.Unmarshal(contents, &record); decodeError != nil {
				t.Fatalf("json.Unmarshal() error = %v", decodeError)
			}

			wantProject, resolveError := filepath.Abs(exampleRoot)
			if resolveError != nil {
				t.Fatalf("filepath.Abs() error = %v", resolveError)
			}
			wantArguments := append(
				[]string{"--inventory", filepath.Join(wantProject, "inventory", "hosts.yml")},
				test.wantTail...,
			)
			if !reflect.DeepEqual(record, wantArguments) {
				t.Errorf("ansible-playbook arguments = %#v, want %#v", record, wantArguments)
			}
		})
	}

	command := exec.CommandContext(t.Context(), python, launcher, "preview", "../site.yml")
	command.Dir = exampleRoot
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 64 {
		t.Fatalf("unsafe playbook error = %v, output = %s", err, output)
	}
	if !strings.Contains(string(output), "playbook path must stay inside") {
		t.Errorf("unsafe playbook output = %q", output)
	}
}

func TestInfrastructureOpsAnsibleLauncherForwardsTermination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group signal behavior is POSIX-specific")
	}

	python, err := python3Executable(t.Context())
	if err != nil {
		t.Skip("python3 is required to test the example Ansible launcher")
	}

	exampleRoot := filepath.Join("..", "..", "examples", "infrastructure-ops", "ansible")
	launcher := filepath.Join("slipramp", "ansible.py")
	executableRoot, err := filepath.Abs(filepath.Join("testdata", "ansible-launcher"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	readyPath := filepath.Join(t.TempDir(), "ready")

	var output bytes.Buffer
	command := exec.CommandContext(t.Context(), python, launcher, "apply", "site.yml")
	command.Dir = exampleRoot
	command.Env = append(
		environmentWithPath(executableRoot),
		"FAKE_ANSIBLE_READY="+readyPath,
	)
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatalf("command.Start() error = %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatalf("ansible-playbook did not start, output = %s", output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Process.Signal() error = %v", err)
	}
	waitError := command.Wait()
	var exitError *exec.ExitError
	if !errors.As(waitError, &exitError) || exitError.ExitCode() != 143 {
		t.Fatalf("launcher termination error = %v, output = %s", waitError, output.String())
	}
}

func environmentWithPath(directory string) []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "PATH=") {
			environment = append(environment, variable)
		}
	}

	return append(environment, "PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func python3Executable(ctx context.Context) (string, error) {
	command := exec.CommandContext(ctx, "python3", "-c", "import sys; print(sys.executable)")
	output, err := command.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}
