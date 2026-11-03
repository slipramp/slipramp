package ansiblereport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const (
	defaultRunnerBinary          = "ansible-runner"
	defaultAnsiblePlaybookBinary = "ansible-playbook"
)

var ansibleCoreVersionPattern = regexp.MustCompile(`(?m)^ansible-playbook \[core ([^\]]+)\]`)

type Config struct {
	Mode             Mode
	ProjectDirectory string
	Inventory        string
	Playbook         string
	Limit            string
	ReportPath       string
	RunnerBinary     string
	PlaybookBinary   string
	Stdout           io.Writer
	Stderr           io.Writer
	Signals          <-chan os.Signal
}

func Run(ctx context.Context, config Config) (int, error) {
	resolved, err := resolveConfig(config)
	if err != nil {
		return 64, err
	}

	repositoryDirectory := findRepositoryDirectory(resolved.ProjectDirectory)
	versions := detectVersions(ctx, resolved.RunnerBinary, resolved.PlaybookBinary)
	temporaryDirectory, err := os.MkdirTemp("", "slipramp-ansible-*")
	if err != nil {
		return 70, fmt.Errorf("create Ansible Runner workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporaryDirectory) }()

	privateDataDirectory := filepath.Join(temporaryDirectory, "private")
	artifactDirectory := filepath.Join(temporaryDirectory, "artifacts")
	runnerArtifactDirectory := filepath.Join(artifactDirectory, defaultRunnerIdentity)
	arguments := []string{
		"run",
		privateDataDirectory,
		"--project-dir",
		resolved.ProjectDirectory,
		"--artifact-dir",
		artifactDirectory,
		"--ident",
		defaultRunnerIdentity,
		"--inventory",
		resolved.Inventory,
		"--playbook",
		resolved.Playbook,
		"--omit-env-files",
	}
	if resolved.Mode == ModePreview {
		arguments = append(arguments, "--cmdline", "--check --diff")
	}
	if resolved.Limit != "" {
		arguments = append(arguments, "--limit", resolved.Limit)
	}

	result, err := runRunner(ctx, resolved, arguments)
	if err != nil {
		return 69, err
	}
	packageErr := PackageSource(PackageConfig{
		Destination:             resolved.ReportPath,
		Mode:                    resolved.Mode,
		RunnerArtifactDirectory: runnerArtifactDirectory,
		RepositoryDirectory:     repositoryDirectory,
		RunnerExitCodeFallback:  result.runnerExitCode,
		Canceled:                result.signal != 0,
		Versions:                versions,
	})
	if packageErr != nil {
		if result.exitCode == 0 {
			result.exitCode = 70
		}
		return result.exitCode, fmt.Errorf("package Ansible report: %w", packageErr)
	}

	return result.exitCode, nil
}

// Standalone playbooks use the project directory; Git checkouts use the nearest repository root.
func findRepositoryDirectory(projectDirectory string) string {
	for directory := projectDirectory; ; directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
			return directory
		}
		if filepath.Dir(directory) == directory {
			return projectDirectory
		}
	}
}

func resolveConfig(config Config) (Config, error) {
	if config.Mode != ModePreview && config.Mode != ModeApply {
		return Config{}, fmt.Errorf("mode must be %q or %q", ModePreview, ModeApply)
	}
	if config.ProjectDirectory == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("resolve project directory: %w", err)
		}
		config.ProjectDirectory = workingDirectory
	}
	projectDirectory, err := filepath.Abs(config.ProjectDirectory)
	if err != nil {
		return Config{}, fmt.Errorf("resolve project directory: %w", err)
	}
	projectDirectory, err = filepath.EvalSymlinks(projectDirectory)
	if err != nil {
		return Config{}, fmt.Errorf("resolve project directory: %w", err)
	}
	config.ProjectDirectory = projectDirectory

	if config.Playbook == "" {
		return Config{}, errors.New("playbook is required")
	}
	if filepath.IsAbs(config.Playbook) {
		return Config{}, errors.New("playbook path must be relative to the project directory")
	}
	playbookPath := filepath.Join(projectDirectory, config.Playbook)
	resolvedPlaybook, err := filepath.EvalSymlinks(playbookPath)
	if err != nil {
		return Config{}, fmt.Errorf("resolve playbook: %w", err)
	}
	playbookRelative, err := filepath.Rel(projectDirectory, resolvedPlaybook)
	if err != nil || playbookRelative == ".." || strings.HasPrefix(playbookRelative, ".."+string(os.PathSeparator)) {
		return Config{}, errors.New("playbook path must stay inside the project directory")
	}
	info, err := os.Stat(resolvedPlaybook)
	if err != nil {
		return Config{}, fmt.Errorf("stat playbook: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Config{}, errors.New("playbook must be a regular file")
	}
	config.Playbook = playbookRelative

	if config.Inventory == "" {
		config.Inventory = filepath.Join("inventory", "hosts.yml")
	}
	if !filepath.IsAbs(config.Inventory) {
		config.Inventory = filepath.Join(projectDirectory, config.Inventory)
	}
	config.Inventory, err = filepath.Abs(config.Inventory)
	if err != nil {
		return Config{}, fmt.Errorf("resolve inventory: %w", err)
	}

	if config.ReportPath == "" {
		config.ReportPath = DefaultReportPath
	}
	config.ReportPath, err = filepath.Abs(config.ReportPath)
	if err != nil {
		return Config{}, fmt.Errorf("resolve report path: %w", err)
	}
	if config.RunnerBinary == "" {
		config.RunnerBinary = defaultRunnerBinary
	}
	if config.PlaybookBinary == "" {
		config.PlaybookBinary = defaultAnsiblePlaybookBinary
	}
	if config.Stdout == nil {
		config.Stdout = io.Discard
	}
	if config.Stderr == nil {
		config.Stderr = io.Discard
	}

	return config, nil
}

type runnerResult struct {
	exitCode       int
	runnerExitCode int
	signal         syscall.Signal
}

func runRunner(ctx context.Context, config Config, arguments []string) (runnerResult, error) {
	command := exec.CommandContext(context.WithoutCancel(ctx), config.RunnerBinary, arguments...)
	command.Dir = config.ProjectDirectory
	command.Stdout = config.Stdout
	command.Stderr = config.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return runnerResult{}, fmt.Errorf("start Ansible Runner: %w", err)
	}

	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
	}()

	var forwardedSignal syscall.Signal
	contextDone := ctx.Done()
	signals := config.Signals
	for {
		select {
		case err := <-wait:
			if err != nil {
				if _, ok := errors.AsType[*exec.ExitError](err); !ok {
					return runnerResult{}, fmt.Errorf("wait for Ansible Runner: %w", err)
				}
			}

			runnerExitCode := command.ProcessState.ExitCode()
			exitCode := runnerExitCode
			if forwardedSignal != 0 {
				exitCode = 128 + int(forwardedSignal)
				if runnerExitCode < 0 {
					runnerExitCode = exitCode
				}
			} else if runnerExitCode < 0 {
				waitStatus, ok := command.ProcessState.Sys().(syscall.WaitStatus)
				if ok && waitStatus.Signaled() {
					forwardedSignal = waitStatus.Signal()
					exitCode = 128 + int(forwardedSignal)
					runnerExitCode = exitCode
				}
			}

			return runnerResult{
				exitCode:       exitCode,
				runnerExitCode: runnerExitCode,
				signal:         forwardedSignal,
			}, nil
		case received, open := <-signals:
			if !open {
				signals = nil
				continue
			}
			signal, ok := received.(syscall.Signal)
			if !ok {
				continue
			}
			if forwardedSignal == 0 {
				forwardedSignal = signal
			}
			if err := syscall.Kill(-command.Process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
				_ = command.Process.Kill()
				<-wait
				return runnerResult{}, fmt.Errorf("forward signal to Ansible Runner: %w", err)
			}
		case <-contextDone:
			contextDone = nil
			if forwardedSignal == 0 {
				forwardedSignal = syscall.SIGTERM
			}
			if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil &&
				!errors.Is(err, syscall.ESRCH) {
				_ = command.Process.Kill()
				<-wait
				return runnerResult{}, fmt.Errorf("cancel Ansible Runner: %w", err)
			}
		}
	}
}

func detectVersions(ctx context.Context, runnerBinary, playbookBinary string) Versions {
	return Versions{
		AnsibleCore:   detectAnsibleCoreVersion(ctx, playbookBinary),
		AnsibleRunner: detectCommandVersion(ctx, runnerBinary),
	}
}

func detectCommandVersion(ctx context.Context, binary string) string {
	output := boundedVersionOutput(ctx, binary, "--version")
	if output == "" {
		return ""
	}

	version, _, _ := strings.Cut(output, "\n")
	return sanitizeVersion(version)
}

func detectAnsibleCoreVersion(ctx context.Context, binary string) string {
	output := boundedVersionOutput(ctx, binary, "--version")
	match := ansibleCoreVersionPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return ""
	}

	return sanitizeVersion(match[1])
}

func boundedVersionOutput(ctx context.Context, binary string, arguments ...string) string {
	versionContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	command := exec.CommandContext(versionContext, binary, arguments...)
	var output bytes.Buffer
	command.Stdout = &boundedWriter{destination: &output, maximum: 4 * 1024}
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return ""
	}

	return output.String()
}

func sanitizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	for _, character := range value {
		if unicode.IsPrint(character) {
			continue
		}
		return ""
	}

	return value
}
