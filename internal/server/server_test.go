package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/healthz",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler().ServeHTTP(response, request)
	expectedBody := "{\"status\":\"ok\"}\n"

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}

	if response.Body.String() != expectedBody {
		t.Errorf(
			"body = %q, want %q",
			response.Body.String(),
			expectedBody,
		)
	}

	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf(
			"Content-Type = %q, want %q",
			contentType,
			"application/json",
		)
	}
}

func TestHealthEndpointRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/healthz",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf(
			"status = %d, want %d",
			response.Code,
			http.StatusMethodNotAllowed,
		)
	}
}

func TestServerStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	listener := newBlockingListener()

	settings := DefaultConfig()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := newServer(settings, logger)

	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)

	go func() {
		serveResult <- app.serve(ctx, listener)
	}()

	select {
	case <-listener.accepting:
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not start accepting connections")
	}

	cancel()

	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not return after its context was canceled")
	}
}

type blockingListener struct {
	accepting chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{
		accepting: make(chan struct{}),
		closed:    make(chan struct{}),
	}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	l.startOnce.Do(func() {
		close(l.accepting)
	})

	<-l.closed

	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})

	return nil
}

func (*blockingListener) Addr() net.Addr {
	return localAddress("in-memory")
}

type localAddress string

func (localAddress) Network() string {
	return "memory"
}

func (a localAddress) String() string {
	return string(a)
}
