package kargotools

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

func normalizeTextArtifact(value []byte) []byte {
	value = bytes.TrimRight(value, "\r\n")
	if len(value) == 0 {
		return []byte{}
	}

	return append(bytes.Clone(value), '\n')
}

func writeArtifacts(gitPath string, gitDiff []byte, argoPath string, argoDiff []byte) error {
	gitTemporary, err := writeTemporaryArtifact(gitPath, gitDiff)
	if err != nil {
		return fmt.Errorf("write Git diff artifact: %w", err)
	}
	defer func() { _ = os.Remove(gitTemporary) }()

	argoTemporary, err := writeTemporaryArtifact(argoPath, argoDiff)
	if err != nil {
		return fmt.Errorf("write Argo CD diff artifact: %w", err)
	}
	defer func() { _ = os.Remove(argoTemporary) }()

	if err := os.Rename(gitTemporary, gitPath); err != nil {
		return fmt.Errorf("install Git diff artifact: %w", err)
	}
	if err := os.Rename(argoTemporary, argoPath); err != nil {
		return fmt.Errorf("install Argo CD diff artifact: %w", err)
	}

	return nil
}

func writeTemporaryArtifact(path string, value []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".slipramp-kargo-artifact-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", err
	}
	if _, err := temporary.Write(value); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", err
	}

	return temporaryPath, nil
}
