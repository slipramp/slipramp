package harness

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestRunConnectsStreamsAndPreservesArguments(t *testing.T) {
	t.Setenv("SLIPRAMP_HARNESS_HELPER", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := Run(
		context.Background(),
		helperCommand("echo", "first argument", "second"),
		Streams{
			Stdin:  strings.NewReader("input"),
			Stdout: &stdout,
			Stderr: &stderr,
		},
		Options{},
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Run() exit code = %d, want 0", exitCode)
	}

	if stdout.String() != "arguments=[\"first argument\" \"second\"] input=\"input\"\n" {
		t.Errorf("stdout = %q", stdout.String())
	}

	if stderr.String() != "helper stderr\n" {
		t.Errorf("stderr = %q, want %q", stderr.String(), "helper stderr\n")
	}
}

func TestRunReturnsActionExitCode(t *testing.T) {
	t.Setenv("SLIPRAMP_HARNESS_HELPER", "1")

	exitCode, err := Run(context.Background(), helperCommand("exit"), Streams{}, Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if exitCode != 42 {
		t.Errorf("Run() exit code = %d, want 42", exitCode)
	}
}

func TestRunForwardsSignals(t *testing.T) {
	t.Setenv("SLIPRAMP_HARNESS_HELPER", "1")

	stdout := newReadyWriter("ready\n")
	signals := make(chan os.Signal, 1)
	result := make(chan runResult, 1)

	go func() {
		exitCode, err := Run(
			context.Background(),
			helperCommand("signal"),
			Streams{Stdout: stdout},
			Options{Signals: signals},
		)
		result <- runResult{exitCode: exitCode, err: err}
	}()

	select {
	case <-stdout.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("action did not become ready")
	}

	signals <- syscall.SIGTERM

	select {
	case received := <-result:
		if received.err != nil {
			t.Fatalf("Run() error = %v", received.err)
		}

		if received.exitCode != 23 {
			t.Errorf("Run() exit code = %d, want 23", received.exitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after forwarding the signal")
	}

	if !strings.Contains(stdout.String(), "signal=terminated\n") {
		t.Errorf("stdout = %q, want forwarded signal", stdout.String())
	}
}

func TestRunForwardsCancellationFile(t *testing.T) {
	t.Setenv("SLIPRAMP_HARNESS_HELPER", "1")

	for _, alreadyCanceled := range []bool{false, true} {
		name := "while running"
		if alreadyCanceled {
			name = "before starting"
		}
		t.Run(name, func(t *testing.T) {
			cancellationFile := t.TempDir() + "/cancellation-requested"
			initialValue := "false"
			command := helperCommand("signal")
			wantExitCode := 23
			pollInterval := time.Millisecond
			if alreadyCanceled {
				initialValue = "true\n"
				command = helperCommand("echo")
				wantExitCode = 128 + int(syscall.SIGTERM)
				pollInterval = time.Second
			}
			if err := os.WriteFile(cancellationFile, []byte(initialValue), 0o600); err != nil {
				t.Fatalf("write cancellation file: %v", err)
			}
			stdout := newReadyWriter("ready\n")
			result := make(chan runResult, 1)

			go func() {
				exitCode, err := Run(
					context.Background(),
					command,
					Streams{Stdout: stdout},
					Options{
						CancellationFile:         cancellationFile,
						cancellationPollInterval: pollInterval,
						cancellationGracePeriod:  time.Second,
					},
				)
				result <- runResult{exitCode: exitCode, err: err}
			}()

			if !alreadyCanceled {
				select {
				case <-stdout.ready:
				case <-time.After(2 * time.Second):
					t.Fatal("action did not become ready")
				}
				if err := os.WriteFile(cancellationFile, []byte("true\n"), 0o600); err != nil {
					t.Fatalf("request cancellation: %v", err)
				}
			}

			select {
			case received := <-result:
				if received.err != nil {
					t.Fatalf("Run() error = %v", received.err)
				}
				if received.exitCode != wantExitCode {
					t.Errorf("Run() exit code = %d, want %d", received.exitCode, wantExitCode)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Run() did not honor the cancellation request")
			}
			if alreadyCanceled && stdout.String() != "" {
				t.Errorf("canceled action started: stdout = %q", stdout.String())
			}
		})
	}
}

func TestRunRejectsMissingInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ctx       context.Context
		command   []string
		wantError string
	}{
		{
			name:      "context",
			command:   []string{"action"},
			wantError: "context",
		},
		{
			name:      "command",
			ctx:       context.Background(),
			wantError: "command",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Run(test.ctx, test.command, Streams{}, Options{})
			testcheck.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestRunReportsStartFailure(t *testing.T) {
	t.Parallel()

	_, err := Run(context.Background(), []string{"/path/that/does/not/exist"}, Streams{}, Options{})
	testcheck.ErrorContains(t, err, "start action")
}

type runResult struct {
	exitCode int
	err      error
}

type readyWriter struct {
	mu        sync.Mutex
	output    strings.Builder
	wantReady string
	ready     chan struct{}
	readyOnce sync.Once
}

func newReadyWriter(wantReady string) *readyWriter {
	return &readyWriter{
		wantReady: wantReady,
		ready:     make(chan struct{}),
	}
}

func (w *readyWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written, err := w.output.Write(value)
	if strings.Contains(w.output.String(), w.wantReady) {
		w.readyOnce.Do(func() { close(w.ready) })
	}

	return written, err
}

func (w *readyWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.output.String()
}

func helperCommand(mode string, arguments ...string) []string {
	command := []string{
		os.Args[0],
		"-test.run=^TestHarnessHelperProcess$",
		"--",
		mode,
	}

	return append(command, arguments...)
}

func TestHarnessHelperProcess(t *testing.T) {
	if os.Getenv("SLIPRAMP_HARNESS_HELPER") != "1" {
		return
	}

	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}

	mode := os.Args[separator+1]
	arguments := os.Args[separator+2:]

	switch mode {
	case "echo":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(90)
		}

		_, _ = fmt.Fprintf(os.Stdout, "arguments=%q input=%q\n", arguments, input)
		_, _ = fmt.Fprintln(os.Stderr, "helper stderr")
		os.Exit(0)
	case "exit":
		os.Exit(42)
	case "signal":
		received := make(chan os.Signal, 1)
		signal.Notify(received, syscall.SIGTERM)
		defer signal.Stop(received)

		_, _ = fmt.Fprintln(os.Stdout, "ready")
		forwarded := <-received
		_, _ = fmt.Fprintf(os.Stdout, "signal=%s\n", forwarded)
		os.Exit(23)
	default:
		os.Exit(91)
	}
}
