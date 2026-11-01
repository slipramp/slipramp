package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/slipramp/slipramp/internal/harness"
)

func TestRunRequiresCommand(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	exitCode := run(nil, nil, nil, &stderr)

	if exitCode != 2 {
		t.Errorf("run() exit code = %d, want 2", exitCode)
	}

	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunHonorsCancellationDuringInputDownload(t *testing.T) {
	directory := t.TempDir()
	cancellationFile := filepath.Join(directory, "cancellation-requested")
	if err := os.WriteFile(cancellationFile, []byte("false"), 0o600); err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(directory, "plan")
	inputs, err := json.Marshal(map[string]harness.InputDeclaration{"plan": {MountPath: inputPath}})
	if err != nil {
		t.Fatal(err)
	}
	contents := []byte("saved plan")
	digest := sha256.Sum256(contents)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if err := os.WriteFile(cancellationFile, []byte("true"), 0o600); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Digest", "sha-256="+hex.EncodeToString(digest[:]))
		_, _ = writer.Write(contents)
	}))
	defer server.Close()
	t.Setenv(harness.ServerURLEnvironment, server.URL)
	t.Setenv(harness.RunIDEnvironment, "run-1")
	t.Setenv(harness.ArtifactTokenEnvironment, "run-token")
	t.Setenv(harness.ArtifactsEnvironment, "{}")
	t.Setenv(harness.ArtifactInputsEnvironment, string(inputs))
	t.Setenv(harness.CancellationFileEnvironment, cancellationFile)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"/bin/sh", "-c", "printf action-started"}, nil, &stdout, &stderr)
	if exitCode != 128+int(syscall.SIGTERM) {
		t.Errorf("run() exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("canceled action started: stdout = %q", stdout.String())
	}
	materialized, err := os.ReadFile(inputPath)
	if err != nil || !bytes.Equal(materialized, contents) {
		t.Errorf("materialized input = %q, error = %v", materialized, err)
	}
}
