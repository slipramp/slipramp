package controller

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewValidatesDependencies(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name      string
		cfg       Config
		logger    *slog.Logger
		wantError string
	}{
		{
			name:      "configuration",
			cfg:       Config{},
			logger:    logger,
			wantError: "cluster name",
		},
		{
			name:      "logger",
			cfg:       validConfig(),
			wantError: "logger",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := New(test.cfg, test.logger)
			if err == nil {
				t.Fatal("New() error = nil, want an error")
			}

			if !strings.Contains(err.Error(), test.wantError) {
				t.Errorf("New() error = %q, want %q", err, test.wantError)
			}
		})
	}
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
