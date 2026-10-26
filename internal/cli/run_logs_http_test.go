package cli_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slipramp/slipramp/internal/cli"
)

func TestRunLogsReconnectsWithoutDuplicateBytes(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		attempt := requests.Add(1)
		switch attempt {
		case 1:
			if got := request.URL.Query().Get("offsetBytes"); got != "0" {
				t.Errorf("first offset = %q", got)
			}
			_, _ = fmt.Fprint(writer, "id: 3\nevent: chunk\ndata: {\"offsetBytes\":0,\"nextOffsetBytes\":3,\"contentsBase64\":\"YWJj\"}\n\n")
		case 2:
			if got := request.URL.Query().Get("offsetBytes"); got != "3" {
				t.Errorf("reconnect offset = %q", got)
			}
			_, _ = fmt.Fprint(
				writer,
				"id: 6\nevent: chunk\ndata: {\"offsetBytes\":0,\"nextOffsetBytes\":6,\"contentsBase64\":\"YWJjZGVm\"}\n\n"+
					"id: 6\nevent: sealed\ndata: {}\n\n",
			)
		default:
			t.Errorf("unexpected request %d", attempt)
		}
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(t, context.Background(), server, "run", "logs", testRunID)
	if code != cli.ExitValid || output != "abcdef" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
	if requests.Load() != 2 || !strings.Contains(stderr, "reconnecting from byte 3") {
		t.Errorf("requests = %d, stderr = %q", requests.Load(), stderr)
	}
}

func TestRunLogsFailedAfterRetainedPrefix(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(
			writer,
			"id: 4\nevent: chunk\ndata: {\"offsetBytes\":0,\"nextOffsetBytes\":4,\"contentsBase64\":\"bG9zdA==\"}\n\n"+
				"id: 4\nevent: failed\ndata: {}\n\n",
		)
	}))
	defer server.Close()

	code, output, stderr := runRemoteApp(t, context.Background(), server, "run", "logs", testRunID)
	if code != cli.ExitIncomplete || output != "lost" || !strings.Contains(stderr, "retained 4 bytes") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}

func TestRunLogsPermanentAndMalformedFailures(t *testing.T) {
	t.Parallel()

	t.Run("unauthorized", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writeProblem(writer, http.StatusUnauthorized, "The API key is invalid.")
		}))
		defer server.Close()

		code, output, stderr := runRemoteApp(t, context.Background(), server, "run", "logs", testRunID)
		if code != cli.ExitRemote || output != "" ||
			!strings.Contains(stderr, "authentication failed (HTTP 401): The API key is invalid.") {
			t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(
				writer,
				"id: 5\nevent: chunk\ndata: {\"offsetBytes\":0,\"nextOffsetBytes\":5,\"contentsBase64\":\"YWJj\"}\n\n",
			)
		}))
		defer server.Close()

		code, output, stderr := runRemoteApp(t, context.Background(), server, "run", "logs", testRunID)
		if code != cli.ExitRemote || output != "" || !strings.Contains(stderr, "inconsistent byte offsets") {
			t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
		}
	})
}

func TestRunLogsRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, output, stderr := runRemoteApp(t, ctx, server, "run", "logs", testRunID)
	if code != cli.ExitRemote || output != "" || !strings.Contains(stderr, "canceled") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, output, stderr)
	}
}
