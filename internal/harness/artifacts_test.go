package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestArtifactTransfersDownloadInputsAndRetryUploads(t *testing.T) {
	t.Parallel()

	var uploadAttempts atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status := http.StatusOK
		headers := http.Header{}
		body := ""
		if request.Header.Get("Authorization") != "Bearer run-token" {
			status = http.StatusUnauthorized
			return testHTTPResponse(request, status, headers, body), nil
		}

		switch request.URL.Path {
		case "/execution/v1/runs/run-1/inputs/plan":
			contents := []byte("saved plan")
			digest := sha256.Sum256(contents)
			headers.Set("Digest", "sha-256="+hex.EncodeToString(digest[:]))
			body = string(contents)
		case "/execution/v1/runs/run-1/artifacts/result":
			attempt := uploadAttempts.Add(1)
			if attempt == 1 {
				status = http.StatusServiceUnavailable
				break
			}
			contents, _ := io.ReadAll(request.Body)
			if string(contents) != "result bytes" ||
				request.Header.Get("X-Slipramp-Content-SHA256") == "" {
				status = http.StatusBadRequest
				break
			}
			status = http.StatusNoContent
		default:
			status = http.StatusNotFound
		}

		return testHTTPResponse(request, status, headers, body), nil
	})}

	directory := t.TempDir()
	inputPath := filepath.Join(directory, "inputs", "plan.tfplan")
	outputPath := filepath.Join(directory, "artifacts", "reports", "result.txt")
	config := ArtifactConfig{
		ServerURL: "https://slipramp.test",
		RunID:     "run-1",
		Token:     "run-token",
		Artifacts: map[string]ArtifactDeclaration{
			"result": {Path: outputPath},
		},
		Inputs: map[string]InputDeclaration{
			"plan": {MountPath: inputPath},
		},
		Client: client,
	}

	if err := config.PrepareArtifactOutputs(); err != nil {
		t.Fatalf("PrepareArtifactOutputs() error = %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("result bytes"), 0o600); err != nil {
		t.Fatalf("write output fixture: %v", err)
	}
	if err := config.MaterializeInputs(context.Background()); err != nil {
		t.Fatalf("MaterializeInputs() error = %v", err)
	}
	materialized, err := os.ReadFile(inputPath)
	if err != nil || string(materialized) != "saved plan" {
		t.Fatalf("materialized input = %q, error = %v", materialized, err)
	}
	if err := config.UploadArtifacts(context.Background()); err != nil {
		t.Fatalf("UploadArtifacts() error = %v", err)
	}
	if uploadAttempts.Load() != 2 {
		t.Errorf("upload attempts = %d, want 2", uploadAttempts.Load())
	}
}

func TestArtifactTransfersUploadEmptyArtifactWithKnownLength(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.ContentLength != 0 {
			t.Errorf("Content-Length = %d, want 0", request.ContentLength)
		}
		if len(request.TransferEncoding) != 0 {
			t.Errorf("Transfer-Encoding = %v, want none", request.TransferEncoding)
		}
		contents, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if len(contents) != 0 {
			t.Errorf("request body = %q, want empty", contents)
		}

		digest := sha256.Sum256(nil)
		if got, want := request.Header.Get("X-Slipramp-Content-SHA256"), hex.EncodeToString(digest[:]); got != want {
			t.Errorf("X-Slipramp-Content-SHA256 = %q, want %q", got, want)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	outputPath := filepath.Join(t.TempDir(), "empty.diff")
	if err := os.WriteFile(outputPath, nil, 0o600); err != nil {
		t.Fatalf("write empty output fixture: %v", err)
	}
	config := ArtifactConfig{
		ServerURL: server.URL,
		RunID:     "run-1",
		Token:     "run-token",
		Artifacts: map[string]ArtifactDeclaration{
			"result": {Path: outputPath},
		},
		Client: server.Client(),
	}

	if err := config.UploadArtifacts(context.Background()); err != nil {
		t.Fatalf("UploadArtifacts() error = %v", err)
	}
}

func TestArtifactTransferErrorsIncludeResponseBodies(t *testing.T) {
	t.Parallel()

	var uploadAttempts atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodGet:
			return testHTTPResponse(
				request,
				http.StatusForbidden,
				http.Header{},
				`{"detail":"input is no longer retained"}`+"\n",
			), nil
		case http.MethodPut:
			uploadAttempts.Add(1)
			return testHTTPResponse(
				request,
				http.StatusInternalServerError,
				http.Header{},
				`{"detail":"artifact storage is unavailable"}`+"\n",
			), nil
		default:
			return testHTTPResponse(request, http.StatusMethodNotAllowed, http.Header{}, ""), nil
		}
	})}

	directory := t.TempDir()
	outputPath := filepath.Join(directory, "result.txt")
	if err := os.WriteFile(outputPath, []byte("result bytes"), 0o600); err != nil {
		t.Fatalf("write output fixture: %v", err)
	}
	config := ArtifactConfig{
		ServerURL: "https://slipramp.test",
		RunID:     "run-1",
		Token:     "run-token",
		Artifacts: map[string]ArtifactDeclaration{
			"result": {Path: outputPath},
		},
		Inputs: map[string]InputDeclaration{
			"plan": {MountPath: filepath.Join(directory, "plan.tfplan")},
		},
		Client: client,
	}

	err := config.MaterializeInputs(context.Background())
	want := `materialize input "plan": server returned HTTP 403: response body "{\"detail\":\"input is no longer retained\"}"`
	if err == nil || err.Error() != want {
		t.Fatalf("MaterializeInputs() error = %v, want %q", err, want)
	}

	err = config.UploadArtifacts(context.Background())
	want = `upload artifact "result": server returned HTTP 500: response body "{\"detail\":\"artifact storage is unavailable\"}"`
	if err == nil || err.Error() != want {
		t.Fatalf("UploadArtifacts() error = %v, want %q", err, want)
	}
	if uploadAttempts.Load() != 3 {
		t.Errorf("upload attempts = %d, want 3", uploadAttempts.Load())
	}
}

func TestServerResponseErrorBoundsBody(t *testing.T) {
	t.Parallel()

	response := testHTTPResponse(
		&http.Request{},
		http.StatusBadGateway,
		http.Header{},
		strings.Repeat("x", maximumErrorResponseBodyBytes+1),
	)
	defer func() { _ = response.Body.Close() }()

	err := serverResponseError(response)
	if err == nil || !strings.HasSuffix(err.Error(), `" (truncated)`) {
		t.Fatalf("serverResponseError() = %v, want bounded truncated body", err)
	}
	if len(err.Error()) > maximumErrorResponseBodyBytes+100 {
		t.Fatalf("serverResponseError() length = %d, want bounded output", len(err.Error()))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func testHTTPResponse(request *http.Request, status int, headers http.Header, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestActionEnvironmentRemovesRunCredential(t *testing.T) {
	t.Parallel()

	filtered := actionEnvironment([]string{
		"PATH=/bin",
		ArtifactTokenEnvironment + "=secret",
		RunIDEnvironment + "=run-1",
	})
	if len(filtered) != 1 || filtered[0] != "PATH=/bin" {
		t.Errorf("filtered environment = %v", filtered)
	}
}
