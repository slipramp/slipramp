package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/run"
)

func TestServerStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	listener := newBlockingListener()
	settings := DefaultConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := newServer(settings, logger, http.NewServeMux())

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

func TestServerShutdownCancelsActiveControllerPoll(t *testing.T) {
	t.Parallel()

	serverConnection, clientConnection := net.Pipe()
	listener := newSingleConnectionListener(serverConnection)
	defer func() {
		_ = listener.Close()
		_ = serverConnection.Close()
		_ = clientConnection.Close()
	}()

	controllers := &blockingClusterService{
		started: make(chan struct{}),
	}
	settings := DefaultConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpHandler := newAdminTestHandler(
		controllers,
		controllerIdentityFor("controller-token", "colo"),
		nil,
		nil,
		&recordingRunService{nextError: run.ErrNoRunAssignment},
		nil,
		nil,
	)
	app := newServer(settings, logger, httpHandler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- app.serve(ctx, listener)
	}()

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://slipramp.test/controller/v1/poll",
		nil,
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	request.Header.Set("Authorization", "Bearer controller-token")

	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConnection, nil
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   2 * time.Second,
	}

	requestResult := make(chan error, 1)
	go func() {
		response, requestErr := client.Do(request)
		if response != nil {
			requestErr = errors.Join(requestErr, response.Body.Close())
		}

		requestResult <- requestErr
	}()

	select {
	case <-controllers.started:
	case <-time.After(2 * time.Second):
		t.Fatal("controller poll did not start")
	}

	cancel()

	select {
	case serveErr := <-serveResult:
		if serveErr != nil {
			t.Fatalf("serve() error = %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not stop after canceling an active poll")
	}

	select {
	case <-requestResult:
	case <-time.After(2 * time.Second):
		t.Fatal("controller poll request did not finish")
	}
}

type blockingClusterService struct {
	started chan struct{}
}

func (p *blockingClusterService) RecordControllerContact(_ context.Context, _ string) error {
	close(p.started)

	return nil
}

func (*blockingClusterService) ListClusters(context.Context) ([]cluster.Status, error) {
	return nil, nil
}

type singleConnectionListener struct {
	connection net.Conn
	closed     chan struct{}
	closeOnce  sync.Once
	mutex      sync.Mutex
	accepted   bool
}

func newSingleConnectionListener(connection net.Conn) *singleConnectionListener {
	return &singleConnectionListener{
		connection: connection,
		closed:     make(chan struct{}),
	}
}

func (l *singleConnectionListener) Accept() (net.Conn, error) {
	l.mutex.Lock()
	if !l.accepted {
		l.accepted = true
		l.mutex.Unlock()

		return l.connection, nil
	}
	l.mutex.Unlock()

	<-l.closed

	return nil, net.ErrClosed
}

func (l *singleConnectionListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})

	return nil
}

func (*singleConnectionListener) Addr() net.Addr {
	return localAddress("in-memory")
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
