package cli_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestArtifactListTextAndJSON(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/runs/"+testRunID+"/artifacts" {
			http.NotFound(writer, request)

			return
		}
		writeJSON(t, writer, http.StatusOK, map[string]any{"items": []map[string]any{{
			"id": testArtifactID, "runId": testRunID, "name": "plan", "kind": "output", "state": "sealed",
			"mediaType": "application/octet-stream", "sizeBytes": 128, "sha256": digest,
			"sensitive": true, "retentionState": "retained", "createdAt": "2026-10-25T12:00:00Z",
			"sealedAt": "2026-10-25T12:01:00Z",
			"provenance": map[string]any{
				"producerRunId":     testRunID,
				"repositoryId":      "018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
				"target":            "storage",
				"action":            "preview",
				"commit":            map[string]any{"sha": strings.Repeat("b", 40)},
				"catalogRevisionId": "018f90ff-9fd8-7b5f-b7e1-f32ff54c1204",
			},
		}}})
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(t, context.Background(), server, "artifact", "list", testRunID)
	if code != cli.ExitValid || stderr != "" {
		t.Fatalf("text exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
	for _, want := range []string{testArtifactID, "plan", "output", "sealed", "retained", "128", "true", digest} {
		if !strings.Contains(output, want) {
			t.Errorf("text output missing %q: %q", want, output)
		}
	}

	code, output, stderr = runRemoteApp(
		t,
		context.Background(),
		server,
		"artifact", "list", testRunID, "--format", "json",
	)
	if code != cli.ExitValid || stderr != "" || !strings.Contains(output, `"name": "plan"`) {
		t.Fatalf("JSON exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}

func TestArtifactDownloadStreamsAndVerifiesContents(t *testing.T) {
	t.Parallel()

	payload := []byte("immutable plan bytes\n")
	digest := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/artifacts/"+testArtifactID+"/download" {
			http.NotFound(writer, request)

			return
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Digest", "sha-256="+hex.EncodeToString(digest[:]))
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"artifact", "download", testArtifactID, "--output", "-",
	)
	if code != cli.ExitValid || output != string(payload) || stderr != "" {
		t.Fatalf("stdout exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}

	outputPath := filepath.Join(t.TempDir(), "plan.tfplan")
	code, output, stderr = runRemoteApp(
		t,
		context.Background(),
		server,
		"artifact", "download", testArtifactID, "--output", outputPath,
	)
	if code != cli.ExitValid || output != "" || stderr != "" {
		t.Fatalf("file exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(contents) != string(payload) {
		t.Errorf("output = %q", contents)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Errorf("permissions = %o", permissions)
	}

	code, _, stderr = runRemoteApp(
		t,
		context.Background(),
		server,
		"artifact", "download", testArtifactID, "--output", outputPath,
	)
	if code != cli.ExitInvocation || !strings.Contains(stderr, "already exists") {
		t.Fatalf("existing output exit = %d, stderr = %q", code, stderr)
	}
}

func TestArtifactDownloadRemovesDigestMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Digest", "sha-256="+strings.Repeat("0", 64))
		_, _ = writer.Write([]byte("different"))
	}))
	defer server.Close()

	outputPath := filepath.Join(t.TempDir(), "invalid.tfplan")
	code, output, stderr := runRemoteApp(
		t,
		context.Background(),
		server,
		"artifact", "download", testArtifactID, "--output", outputPath,
	)
	if code != cli.ExitRemote || output != "" || !strings.Contains(stderr, "digest does not match") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output remains after digest mismatch: %v", err)
	}
}
