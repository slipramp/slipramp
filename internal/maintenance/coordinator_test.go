package maintenance

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"testing"
	"time"

	clocktesting "k8s.io/utils/clock/testing"
)

func TestCoordinatorRunsDueJobsInRegistrationOrder(t *testing.T) {
	start := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	fakeClock := clocktesting.NewFakeClock(start)
	calls := make(chan string, 5)
	firstFailure := true
	jobs := []Job{
		{
			Name:       "first",
			Interval:   time.Minute,
			Timeout:    time.Second,
			RunOnStart: true,
			Run: func(context.Context) error {
				calls <- "first"
				if firstFailure {
					firstFailure = false
					return errors.New("temporary failure")
				}
				return nil
			},
		},
		{
			Name:       "second",
			Interval:   2 * time.Minute,
			Timeout:    time.Second,
			RunOnStart: true,
			Run: func(context.Context) error {
				calls <- "second"
				return nil
			},
		},
	}
	coordinator, err := NewCoordinator(testLogger(), jobs...)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	coordinator.clock = fakeClock

	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{})
	go func() {
		coordinator.Run(ctx)
		close(finished)
	}()

	assertMaintenanceCall(t, calls, "first")
	assertMaintenanceCall(t, calls, "second")
	waitForMaintenanceTimer(t, fakeClock)
	fakeClock.Step(time.Minute)
	assertMaintenanceCall(t, calls, "first")
	waitForMaintenanceTimer(t, fakeClock)
	fakeClock.Step(time.Minute)
	assertMaintenanceCall(t, calls, "first")
	assertMaintenanceCall(t, calls, "second")

	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Coordinator did not stop after cancellation")
	}
}

func waitForMaintenanceTimer(t *testing.T, fakeClock *clocktesting.FakeClock) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for fakeClock.Waiters() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Coordinator did not schedule its next maintenance run")
		}
		runtime.Gosched()
	}
}

func TestCoordinatorRejectsInvalidJobs(t *testing.T) {
	_, err := NewCoordinator(testLogger(),
		Job{Name: "duplicate", Interval: time.Minute, Timeout: time.Minute, Run: func(context.Context) error { return nil }},
		Job{Name: "duplicate", Interval: time.Minute, Timeout: time.Minute, Run: func(context.Context) error { return nil }},
	)
	if err == nil {
		t.Fatal("NewCoordinator() error = nil")
	}
}

func assertMaintenanceCall(t *testing.T, calls <-chan string, want string) {
	t.Helper()

	select {
	case got := <-calls:
		if got != want {
			t.Fatalf("maintenance call = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("maintenance call %q did not run", want)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
