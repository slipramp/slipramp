// Package harness runs an action process inside a Slipramp execution Job.
package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// Streams are connected directly to the action process.
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run starts command without an implicit shell, forwards received signals, and
// returns the action's exit code. An error indicates that the action could not
// be started or observed reliably, rather than an action failure.
func Run(
	ctx context.Context,
	command []string,
	streams Streams,
	signals <-chan os.Signal,
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

	if err := process.Start(); err != nil {
		return 0, fmt.Errorf("start action: %w", err)
	}

	waitResult := make(chan error, 1)
	go func() {
		waitResult <- process.Wait()
	}()

	var signalError error

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
		}
	}
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
