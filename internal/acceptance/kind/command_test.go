package kindacceptance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type commandResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

type forward struct {
	command *exec.Cmd
	logFile *os.File
}

func (s *suite) command(t *testing.T, input []byte, environment []string, visible bool, name string, args ...string) commandResult {
	t.Helper()

	t.Logf("$ %s %s", name, strings.Join(args, " "))
	command := exec.CommandContext(t.Context(), name, args...)
	command.Dir = s.rootDirectory
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	if environment != nil {
		command.Env = append(os.Environ(), environment...)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if visible {
		command.Stdout = io.MultiWriter(os.Stdout, &stdout)
		command.Stderr = io.MultiWriter(os.Stderr, &stderr)
	}

	err := command.Run()
	result := commandResult{stdout: stdout.Bytes(), stderr: stderr.Bytes()}
	if err == nil {
		return result
	}

	if exitError, ok := errors.AsType[*exec.ExitError](err); ok {
		result.exitCode = exitError.ExitCode()
		return result
	}
	t.Fatalf("run %s: %v", name, err)
	return commandResult{}
}

func (s *suite) mustRun(t *testing.T, name string, args ...string) []byte {
	t.Helper()

	result := s.command(t, nil, nil, false, name, args...)
	if result.exitCode != 0 {
		t.Fatalf("%s failed with exit code %d\nstdout:\n%s\nstderr:\n%s", name, result.exitCode, result.stdout, result.stderr)
	}
	return result.stdout
}

func (s *suite) mustRunVisible(t *testing.T, name string, args ...string) {
	t.Helper()

	result := s.command(t, nil, nil, true, name, args...)
	if result.exitCode != 0 {
		t.Fatalf("%s failed with exit code %d", name, result.exitCode)
	}
}

func (s *suite) mustRunInput(t *testing.T, input []byte, name string, args ...string) []byte {
	t.Helper()

	result := s.command(t, input, nil, false, name, args...)
	if result.exitCode != 0 {
		t.Fatalf("%s failed with exit code %d\nstdout:\n%s\nstderr:\n%s", name, result.exitCode, result.stdout, result.stderr)
	}
	return result.stdout
}

func (s *suite) kubectl(t *testing.T, args ...string) []byte {
	t.Helper()

	arguments := append([]string{}, s.kubectlArguments...)
	arguments = append(arguments, args...)
	return s.mustRun(t, s.kubectlBin, arguments...)
}

func (s *suite) kubectlResult(t *testing.T, args ...string) commandResult {
	t.Helper()

	arguments := append([]string{}, s.kubectlArguments...)
	arguments = append(arguments, args...)
	return s.command(t, nil, nil, false, s.kubectlBin, arguments...)
}

func (s *suite) kubectlApplyInput(t *testing.T, manifest []byte) {
	t.Helper()

	arguments := append([]string{}, s.kubectlArguments...)
	arguments = append(arguments, "apply", "-f", "-")
	s.mustRunInput(t, manifest, s.kubectlBin, arguments...)
}

func (s *suite) startForward(t *testing.T, name, service string, localPort, remotePort int) {
	t.Helper()

	s.stopForward(t, name)
	logPath := filepath.Join(s.temporaryDirectory, name+"-port-forward.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create %s port-forward log: %v", name, err)
	}

	arguments := append([]string{}, s.kubectlArguments...)
	arguments = append(arguments,
		"-n", systemNamespace,
		"port-forward", "service/"+service,
		fmt.Sprintf("%d:%d", localPort, remotePort),
	)
	command := exec.CommandContext(s.processContext, s.kubectlBin, arguments...)
	command.Dir = s.rootDirectory
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start %s port forward: %v", name, err)
	}
	s.forwards[name] = &forward{command: command, logFile: logFile}

	url := fmt.Sprintf("http://127.0.0.1:%d/", localPort)
	if name == "server" {
		url += "readyz"
	}
	if name == "oidc" {
		url += "dex/.well-known/openid-configuration"
	}
	s.waitForHTTP(t, url)
}

func (s *suite) stopForward(t *testing.T, name string) {
	t.Helper()

	forward := s.forwards[name]
	if forward == nil {
		return
	}
	delete(s.forwards, name)
	if forward.command.Process != nil {
		_ = forward.command.Process.Kill()
		_ = forward.command.Wait()
	}
	if err := forward.logFile.Close(); err != nil {
		t.Logf("close %s port-forward log: %v", name, err)
	}
}

func (s *suite) waitForHTTP(t *testing.T, url string) {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastError error
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("create readiness request: %v", err)
		}
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 400 {
				return
			}
			lastError = fmt.Errorf("HTTP status %s", response.Status)
		} else {
			lastError = err
		}
		wait(t, time.Second)
	}
	t.Fatalf("timed out waiting for %s: %v", url, lastError)
}

func wait(t *testing.T, duration time.Duration) {
	t.Helper()

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-t.Context().Done():
		t.Fatalf("acceptance context ended while waiting: %v", context.Cause(t.Context()))
	case <-timer.C:
	}
}
