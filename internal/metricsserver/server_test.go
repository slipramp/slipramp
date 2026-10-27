package metricsserver

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

func TestServerRoutesOnlyMetrics(t *testing.T) {
	t.Parallel()

	metrics := New(
		DefaultAddress,
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "metrics", method: http.MethodGet, path: "/metrics", wantStatus: http.StatusNoContent},
		{name: "wrong method", method: http.MethodPost, path: "/metrics", wantStatus: http.StatusMethodNotAllowed},
		{name: "other path", method: http.MethodGet, path: "/", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response := httptest.NewRecorder()
			metrics.server.Handler.ServeHTTP(
				response,
				httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil),
			)
			if response.Code != test.wantStatus {
				t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.wantStatus)
			}
		})
	}
}

func TestServerStopsWithContext(t *testing.T) {
	t.Parallel()

	listener := newBlockingListener()
	metrics := New(
		DefaultAddress,
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- metrics.serve(ctx, listener)
	}()

	select {
	case <-listener.accepted:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("metrics server did not start accepting connections")
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("metrics server did not stop after context cancellation")
	}
}

func TestValidateAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{name: "all interfaces", address: DefaultAddress},
		{name: "specific host", address: "127.0.0.1:9191"},
		{name: "empty", address: "", wantErr: true},
		{name: "missing port", address: "localhost", wantErr: true},
		{name: "zero port", address: ":0", wantErr: true},
		{name: "out of range", address: ":65536", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateAddress(test.address)
			if (err != nil) != test.wantErr {
				t.Errorf("ValidateAddress(%q) error = %v, want error %t", test.address, err, test.wantErr)
			}
		})
	}
}

type blockingListener struct {
	accepted   chan struct{}
	closed     chan struct{}
	acceptOnce sync.Once
	closeOnce  sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{
		accepted: make(chan struct{}),
		closed:   make(chan struct{}),
	}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	l.acceptOnce.Do(func() { close(l.accepted) })
	<-l.closed

	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })

	return nil
}

func (*blockingListener) Addr() net.Addr {
	return testAddress("in-memory")
}

type testAddress string

func (testAddress) Network() string { return "tcp" }

func (a testAddress) String() string { return string(a) }
