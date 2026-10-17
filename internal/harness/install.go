package harness

import (
	"fmt"
	"io"
	"os"
)

// Install copies the running harness executable to destination. Execution Jobs
// use this from an init container so runtime images do not need to contain the
// Slipramp harness.
func Install(destination string) error {
	if destination == "" {
		return fmt.Errorf("install destination is required")
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate harness executable: %w", err)
	}

	source, err := os.Open(executable)
	if err != nil {
		return fmt.Errorf("open harness executable: %w", err)
	}
	defer func() {
		_ = source.Close()
	}()

	destinationFile, err := os.OpenFile(
		destination,
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
		0o755,
	)
	if err != nil {
		return fmt.Errorf("create installed harness: %w", err)
	}

	if _, err := io.Copy(destinationFile, source); err != nil {
		_ = destinationFile.Close()
		return fmt.Errorf("copy installed harness: %w", err)
	}

	if err := destinationFile.Close(); err != nil {
		return fmt.Errorf("close installed harness: %w", err)
	}

	if err := os.Chmod(destination, 0o755); err != nil {
		return fmt.Errorf("make installed harness executable: %w", err)
	}

	return nil
}
