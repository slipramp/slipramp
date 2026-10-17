package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallCopiesExecutable(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "slipramp-harness")
	if err := Install(destination); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	info, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("stat installed harness: %v", err)
	}
	if info.Size() == 0 {
		t.Error("installed harness is empty")
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("installed mode = %o, want 755", info.Mode().Perm())
	}
}

func TestInstallRequiresDestination(t *testing.T) {
	t.Parallel()

	if err := Install(""); err == nil {
		t.Fatal("Install() error = nil, want an error")
	}
}
