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

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/testcheck"
)

type assignmentPollerFunc func(ctx context.Context) (*api.RunAssignment, error)

func (f assignmentPollerFunc) Poll(ctx context.Context) (*api.RunAssignment, error) {
	return f(ctx)
}

func TestNewRequiresLogger(t *testing.T) {
	t.Parallel()

	_, err := New(validConfig(), nil, fake.NewClientset().BatchV1())
	testcheck.ErrorContains(t, err, "logger")
}

func TestNewRequiresKubernetesJobClient(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := New(validConfig(), logger, nil)
	testcheck.ErrorContains(t, err, "kubernetes Job client")
}

func TestControllerStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	app, err := New(validConfig(), logger, fake.NewClientset().BatchV1())
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
	app, err := New(validConfig(), logger, fake.NewClientset().BatchV1())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	app.server = assignmentPollerFunc(func(context.Context) (*api.RunAssignment, error) {
		return nil, errControllerCredentialRejected
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

func TestControllerBacksOffAfterReceivingWork(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	app, err := New(validConfig(), logger, fake.NewClientset().BatchV1())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	received := make(chan struct{})
	app.server = assignmentPollerFunc(func(context.Context) (*api.RunAssignment, error) {
		select {
		case <-received:
		default:
			close(received)
		}

		return &api.RunAssignment{ID: runID}, nil
	})
	app.retryDelay = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- app.Run(ctx)
	}()

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("controller did not receive a run assignment")
	}
	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop while a run assignment was pending")
	}

	if !strings.Contains(logs.String(), runID.String()) {
		t.Errorf("logs = %q, want Run ID", logs.String())
	}
}
