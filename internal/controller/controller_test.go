package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/testcheck"
)

type serverPollerFunc func(ctx context.Context) error

func (f serverPollerFunc) Poll(ctx context.Context) error {
	return f(ctx)
}

func TestNewRequiresLogger(t *testing.T) {
	t.Parallel()

	_, err := New(validConfig(), nil)
	testcheck.ErrorContains(t, err, "logger")
}

func TestControllerStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	app, err := New(validConfig(), logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)

	go func() {
		result <- app.Run(ctx)
	}()

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after its context was canceled")
	}

	output := logs.String()
	if !strings.Contains(output, "controller started") {
		t.Errorf("logs = %q, want start message", output)
	}

	if !strings.Contains(output, "controller stopped") {
		t.Errorf("logs = %q, want stop message", output)
	}

	if strings.Contains(output, validConfig().BearerToken) {
		t.Errorf("logs contain bearer token: %q", output)
	}
}

func TestControllerStopsAfterAuthenticationIsRejected(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(validConfig(), logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	app.server = serverPollerFunc(func(context.Context) error {
		return errControllerCredentialRejected
	})

	err = app.Run(context.Background())
	if !errors.Is(err, errControllerCredentialRejected) {
		t.Fatalf(
			"Run() error = %v, want %v",
			err,
			errControllerCredentialRejected,
		)
	}
}
