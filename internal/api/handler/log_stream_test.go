package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/artifact"
)

func TestRunLogSSEFramesAndReconnectCursors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		offset     *int64
		lastID     string
		wantOffset int64
		drained    bool
		badHeader  bool
		largeChunk bool
	}{
		{name: "initial stream", largeChunk: true},
		{name: "last event ID", lastID: "11", wantOffset: 11},
		{name: "explicit offset takes precedence", offset: new(int64(3)), lastID: "11", wantOffset: 3},
		{name: "drained cursor", offset: new(int64(23)), wantOffset: 23, drained: true},
		{name: "malformed last event ID", lastID: "invalid", badHeader: true},
		{name: "malformed header with explicit offset", offset: new(int64(3)), lastID: "invalid", badHeader: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runID := uuid.New()
			contents := []byte{0, 0xff, '\n'}
			if test.largeChunk {
				contents = bytes.Repeat(contents, 1400)
			}
			nextOffset := test.wantOffset
			if !test.drained {
				nextOffset += int64(len(contents))
			}
			sent := false
			closed := make(chan struct{})
			var closeOnce sync.Once
			stream := &handlerLogStream{
				next: func() (artifact.LogStreamChunk, error) {
					if sent || test.drained {
						return artifact.LogStreamChunk{}, io.EOF
					}
					sent = true
					return artifact.LogStreamChunk{
						OffsetBytes: test.wantOffset, NextOffsetBytes: nextOffset, Contents: contents,
					}, nil
				},
				close: func() error {
					closeOnce.Do(func() { close(closed) })
					return nil
				},
			}
			artifacts := &recordingArtifactService{stream: stream}
			path := "/api/v1/runs/" + runID.String() + "/logs"
			if test.offset != nil {
				path += "?offsetBytes=" + strconv.FormatInt(*test.offset, 10)
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
			if test.lastID != "" {
				request.Header.Set("Last-Event-ID", test.lastID)
			}
			response := httptest.NewRecorder()
			newAdminTestHandler(nil, nil, nil, nil, nil, artifacts, nil).ServeHTTP(response, request)
			if test.badHeader {
				if response.Code != http.StatusBadRequest || artifacts.streamCalls != 0 {
					t.Fatalf("malformed cursor response = %d, stream calls %d", response.Code, artifacts.streamCalls)
				}
				return
			}
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" ||
				!response.Flushed || artifacts.streamCalls != 1 || artifacts.streamRunID != runID ||
				artifacts.streamOffset != test.wantOffset {
				t.Fatalf("stream response = %d, headers %v, offset %d", response.Code, response.Header(), artifacts.streamOffset)
			}
			frames := strings.Split(strings.TrimSuffix(response.Body.String(), "\n\n"), "\n\n")
			if !test.drained {
				if len(frames) != 2 {
					t.Fatalf("SSE frames = %q", frames)
				}
				prefix := fmt.Sprintf("id: %d\nevent: chunk\ndata: ", nextOffset)
				payload, found := strings.CutPrefix(frames[0], prefix)
				if !found {
					t.Fatalf("chunk frame = %q", frames[0])
				}
				var event api.LogEvent
				if err := json.Unmarshal([]byte(payload), &event); err != nil {
					t.Fatal(err)
				}
				if event.OffsetBytes != test.wantOffset || event.NextOffsetBytes != nextOffset ||
					!bytes.Equal(event.ContentsBase64, contents) {
					t.Errorf("decoded chunk = %#v, want original raw bytes %v", event, contents)
				}
				frames = frames[1:]
			}
			if len(frames) != 1 || frames[0] != fmt.Sprintf("id: %d\nevent: sealed\ndata: {}", nextOffset) {
				t.Errorf("sealed frame = %q", frames)
			}
			select {
			case <-closed:
			default:
				t.Error("completed HTTP response did not close the log stream")
			}
		})
	}
}

func TestRunLogSSECloseAndCancellationInterruptPendingRead(t *testing.T) {
	t.Parallel()

	for _, closeBody := range []bool{true, false} {
		t.Run(fmt.Sprintf("closeBody=%t", closeBody), func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan struct{})
			closed := make(chan struct{})
			var closeOnce sync.Once
			readFailure := context.Canceled
			if closeBody {
				readFailure = fmt.Errorf("read log storage: %w", io.EOF)
			}
			stream := &handlerLogStream{
				next: func() (artifact.LogStreamChunk, error) {
					close(started)
					select {
					case <-ctx.Done():
					case <-closed:
					}
					return artifact.LogStreamChunk{}, readFailure
				},
				close: func() error {
					closeOnce.Do(func() { close(closed) })
					return nil
				},
			}
			handler := newHandler(Dependencies{Artifacts: &recordingArtifactService{stream: stream}})
			result, err := handler.StreamRunLog(ctx, api.StreamRunLogRequestObject{RunID: uuid.New()})
			if err != nil {
				t.Fatal(err)
			}
			response := result.(api.StreamRunLog200TextEventStreamResponse)
			body := response.Body.(io.ReadCloser)
			defer func() { _ = body.Close() }()
			writer := httptest.NewRecorder()
			finished := make(chan error, 1)
			go func() { finished <- response.VisitStreamRunLogResponse(writer) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("log response did not start reading")
			}
			if closeBody {
				if err := body.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-finished:
				if !errors.Is(err, readFailure) || writer.Body.Len() != 0 {
					t.Errorf("interrupted response = %q, %v; want source error without a sealed event", writer.Body.String(), err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("log response remained blocked")
			}
			select {
			case <-closed:
			default:
				t.Error("interrupted HTTP response did not close the log stream")
			}
		})
	}
}

type handlerLogStream struct {
	next  func() (artifact.LogStreamChunk, error)
	close func() error
}

func (s *handlerLogStream) Next() (artifact.LogStreamChunk, error) { return s.next() }

func (s *handlerLogStream) Close() error { return s.close() }
