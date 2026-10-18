// Package harness runs an action process inside a Slipramp execution Job.
package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	CancellationFileEnvironment = "SLIPRAMP_CANCELLATION_FILE"
	cancellationPollInterval    = 250 * time.Millisecond
	cancellationGracePeriod     = 30 * time.Second
)

// Streams are connected directly to the action process.
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type Options struct {
	Signals          <-chan os.Signal
	CancellationFile string

	cancellationPollInterval time.Duration
	cancellationGracePeriod  time.Duration
}

// Run starts command without an implicit shell, forwards received signals, and
// returns the action's exit code. An error indicates that the action could not
// be started or observed reliably, rather than an action failure.
func Run(
	ctx context.Context,
	command []string,
	streams Streams,
	options Options,
) (int, error) {
	if ctx == nil {
		return 0, errors.New("context is required")
	}

	if len(command) == 0 {
		return 0, errors.New("action command is required")
	}

	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Stdin = streams.Stdin
	process.Stdout = streams.Stdout
	process.Stderr = streams.Stderr
	process.Env = actionEnvironment(os.Environ())

	if err := process.Start(); err != nil {
		return 0, fmt.Errorf("start action: %w", err)
	}

	waitResult := make(chan error, 1)
	go func() {
		waitResult <- process.Wait()
	}()

	var signalError error
	signals := options.Signals
	cancellationChecks, stopCancellationChecks := cancellationTicker(options)
	defer stopCancellationChecks()

	var forceKill <-chan time.Time
	var forceKillTimer *time.Timer
	defer func() {
		if forceKillTimer != nil {
			forceKillTimer.Stop()
		}
	}()

	for {
		select {
		case err := <-waitResult:
			exitCode, err := exitResult(err)
			return exitCode, errors.Join(err, signalError)
		case received, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}

			if received == nil {
				continue
			}

			if err := process.Process.Signal(received); err != nil &&
				!errors.Is(err, os.ErrProcessDone) {
				signalError = errors.Join(
					signalError,
					fmt.Errorf("forward signal %s: %w", received, err),
				)
			}
		case <-cancellationChecks:
			requested, err := cancellationRequested(options.CancellationFile)
			if err != nil {
				signalError = errors.Join(
					signalError,
					fmt.Errorf("read cancellation file: %w", err),
				)
				continue
			}
			if !requested {
				continue
			}

			cancellationChecks = nil
			if err := process.Process.Signal(syscall.SIGTERM); err != nil &&
				!errors.Is(err, os.ErrProcessDone) {
				signalError = errors.Join(
					signalError,
					fmt.Errorf("forward cancellation signal: %w", err),
				)
			}
			gracePeriod := options.cancellationGracePeriod
			if gracePeriod <= 0 {
				gracePeriod = cancellationGracePeriod
			}
			forceKillTimer = time.NewTimer(gracePeriod)
			forceKill = forceKillTimer.C
		case <-forceKill:
			forceKill = nil
			if err := process.Process.Kill(); err != nil &&
				!errors.Is(err, os.ErrProcessDone) {
				signalError = errors.Join(
					signalError,
					fmt.Errorf("force canceled action to stop: %w", err),
				)
			}
		}
	}
}

func cancellationTicker(options Options) (<-chan time.Time, func()) {
	if options.CancellationFile == "" {
		return nil, func() {}
	}

	interval := options.cancellationPollInterval
	if interval <= 0 {
		interval = cancellationPollInterval
	}
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func cancellationRequested(path string) (bool, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(string(contents)) == "true", nil
}

func exitResult(err error) (int, error) {
	if err == nil {
		return 0, nil
	}

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return 0, fmt.Errorf("wait for action: %w", err)
	}

	if exitCode := exitError.ExitCode(); exitCode >= 0 {
		return exitCode, nil
	}

	waitStatus, ok := exitError.Sys().(syscall.WaitStatus)
	if ok && waitStatus.Signaled() {
		return 128 + int(waitStatus.Signal()), nil
	}

	return 1, nil
}
