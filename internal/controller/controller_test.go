package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controller/runjob"
)

type commandPollerFunc func(context.Context) (*api.ControllerCommand, error)

func (f commandPollerFunc) Poll(ctx context.Context) (*api.ControllerCommand, error) {
	return f(ctx)
}

type controllerEnrollerFunc func(context.Context) error

func (f controllerEnrollerFunc) Enroll(ctx context.Context) error { return f(ctx) }

type componentRunnerFunc func(context.Context) error

func (f componentRunnerFunc) Run(ctx context.Context) error { return f(ctx) }

type runObservationReporterFunc func(context.Context, uuid.UUID, api.RunObservation) error

func (f runObservationReporterFunc) ReportObservation(
	ctx context.Context,
	runID uuid.UUID,
	observation api.RunObservation,
) error {
	return f(ctx, runID, observation)
}

type stubRunJobs struct {
	ensure              func(context.Context, api.RunAssignment) (runjob.Ensured, error)
	requestCancellation func(context.Context, api.CancelRunCommand) (runjob.CancellationResult, error)
	recover             func(context.Context) ([]runjob.Handle, error)
	observe             func(context.Context, runjob.Handle) (runjob.Observation, error)
	openLog             func(context.Context, runjob.Handle, bool) (io.ReadCloser, error)
	delete              func(context.Context, runjob.Handle) error
}

func (s *stubRunJobs) Ensure(ctx context.Context, assignment api.RunAssignment) (runjob.Ensured, error) {
	if s.ensure == nil {
		return runjob.Ensured{}, nil
	}

	return s.ensure(ctx, assignment)
}

func (s *stubRunJobs) RequestCancellation(
	ctx context.Context,
	command api.CancelRunCommand,
) (runjob.CancellationResult, error) {
	return s.requestCancellation(ctx, command)
}

func (s *stubRunJobs) Recover(ctx context.Context) ([]runjob.Handle, error) {
	if s.recover == nil {
		return nil, nil
	}

	return s.recover(ctx)
}

func (s *stubRunJobs) Observe(ctx context.Context, handle runjob.Handle) (runjob.Observation, error) {
	return s.observe(ctx, handle)
}

func (s *stubRunJobs) OpenLog(ctx context.Context, handle runjob.Handle, follow bool) (io.ReadCloser, error) {
	if s.openLog == nil {
		return nil, runjob.ErrLogNotReady
	}

	return s.openLog(ctx, handle, follow)
}

func (s *stubRunJobs) Delete(ctx context.Context, handle runjob.Handle) error {
	if s.delete == nil {
		return nil
	}

	return s.delete(ctx, handle)
}

func TestControllerStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	app, err := New(validConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), fake.NewClientset(), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	app.enrollment = controllerEnrollerFunc(func(context.Context) error { return nil })
	app.server = commandPollerFunc(func(ctx context.Context) (*api.ControllerCommand, error) {
		<-ctx.Done()

		return nil, ctx.Err()
	})

	result := make(chan error, 1)
	go func() { result <- app.Run(ctx) }()
	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}

func TestRunWorkerLimitBoundsNewAssignments(t *testing.T) {
	t.Parallel()

	ensureCalls := make(chan uuid.UUID, 2)
	jobs := &stubRunJobs{ensure: func(ctx context.Context, assignment api.RunAssignment) (runjob.Ensured, error) {
		ensureCalls <- assignment.ID
		<-ctx.Done()

		return runjob.Ensured{}, ctx.Err()
	}}
	app := newTestController(jobs, &memoryLogReporter{})
	app.runSlots = make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	firstID := uuid.New()
	secondID := uuid.New()
	app.runWorkers.Add(2)
	go app.executeRun(
		ctx,
		runjob.Handle{RunID: firstID},
		&api.RunAssignment{ID: firstID},
		&runWorker{cancellations: make(chan api.CancelRunCommand, 1), done: make(chan struct{})},
	)
	if got := <-ensureCalls; got != firstID {
		t.Fatalf("first materialized Run = %s, want %s", got, firstID)
	}

	go app.executeRun(
		ctx,
		runjob.Handle{RunID: secondID},
		&api.RunAssignment{ID: secondID},
		&runWorker{cancellations: make(chan api.CancelRunCommand, 1), done: make(chan struct{})},
	)
	select {
	case got := <-ensureCalls:
		t.Fatalf("materialized Run %s while the worker limit was full", got)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	app.runWorkers.Wait()
}

func TestControllerStopsWhenMetricsServerFails(t *testing.T) {
	t.Parallel()

	want := errors.New("metrics listener failed")
	app := newTestController(&stubRunJobs{}, &memoryLogReporter{})
	app.metrics = componentRunnerFunc(func(context.Context) error { return want })
	app.enrollment = controllerEnrollerFunc(func(ctx context.Context) error {
		<-ctx.Done()

		return nil
	})

	if err := app.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
}

func TestControllerRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		enrollment error
		poll       error
		want       error
	}{
		{
			name:       "enrollment credential",
			enrollment: errControllerEnrollmentCredentialRejected,
			want:       errControllerEnrollmentCredentialRejected,
		},
		{
			name: "controller credential",
			poll: errControllerCredentialRejected,
			want: errControllerCredentialRejected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			app := newTestController(&stubRunJobs{}, &memoryLogReporter{})
			app.enrollment = controllerEnrollerFunc(func(context.Context) error { return test.enrollment })
			app.server = commandPollerFunc(func(context.Context) (*api.ControllerCommand, error) {
				return nil, test.poll
			})

			err := app.Run(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("Run() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestControllerInterruptsPollAfterWorkerAuthenticationFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	handle := runjob.Handle{RunID: uuid.New(), Namespace: "slipramp-runs", UID: "job-uid"}
	polling := make(chan struct{})
	jobs := &stubRunJobs{
		recover: func(context.Context) ([]runjob.Handle, error) { return []runjob.Handle{handle}, nil },
		observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
			return runjob.Observation{Phase: api.RunObservationPhaseRunning, JobUID: &handle.UID}, nil
		},
	}
	app := newTestController(jobs, &memoryLogReporter{})
	app.server = commandPollerFunc(func(ctx context.Context) (*api.ControllerCommand, error) {
		close(polling)
		<-ctx.Done()

		return nil, ctx.Err()
	})
	app.reports = runObservationReporterFunc(func(ctx context.Context, _ uuid.UUID, _ api.RunObservation) error {
		select {
		case <-polling:
			return newServerResponseError("report Run observation", 401)
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	if err := app.Run(ctx); !errors.Is(err, errControllerCredentialRejected) {
		t.Fatalf("Run() error = %v, want credential rejection", err)
	}
	if ctx.Err() != nil {
		t.Fatal("Controller waited for the parent deadline instead of interrupting polling")
	}
}

func TestControllerReportsMaterializationFailureWithSealedEmptyLog(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	logs := &memoryLogReporter{sealErrors: []error{errors.New("storage unavailable")}}
	var reported api.RunObservation
	reportAttempts := 0
	client := fake.NewClientset()
	jobs := runjob.New(runjob.Config{
		ClusterName:         "colo",
		ExecutionNamespaces: []string{""},
	}, client.BatchV1(), client.CoreV1())
	app := newTestController(jobs, logs)
	app.reports = runObservationReporterFunc(func(
		_ context.Context,
		_ uuid.UUID,
		observation api.RunObservation,
	) error {
		if !logs.isSealed() {
			t.Error("materialization failure reported before its empty log was sealed")
		}
		reportAttempts++
		if reportAttempts == 1 {
			return errors.New("response lost")
		}
		reported = observation

		return nil
	})

	if err := app.handleAssignment(context.Background(), api.RunAssignment{ID: runID}); err != nil {
		t.Fatalf("handleAssignment() error = %v", err)
	}
	app.runWorkers.Wait()
	if reported.Outcome == nil ||
		*reported.Outcome != api.RunObservationOutcomeFailed ||
		reported.Reason == nil || *reported.Reason != "MaterializationFailed" {
		t.Fatalf("reported observation = %#v", reported)
	}
	if contents := logs.bytes(); len(contents) != 0 {
		t.Errorf("materialization log = %q, want empty", contents)
	}
}

func TestControllerPollsWhileRunWorkerIsBusy(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runID := uuid.New()
	started := make(chan struct{})
	var ensureCalls atomic.Int32
	jobs := &stubRunJobs{ensure: func(ctx context.Context, _ api.RunAssignment) (runjob.Ensured, error) {
		if ensureCalls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()

		return runjob.Ensured{}, ctx.Err()
	}}
	app := newTestController(jobs, &memoryLogReporter{})
	polls := 0
	app.server = commandPollerFunc(func(ctx context.Context) (*api.ControllerCommand, error) {
		polls++
		if polls == 1 {
			return &api.ControllerCommand{
				Type:       api.ControllerCommandTypeRunAssignment,
				Assignment: &api.RunAssignment{ID: runID},
			}, nil
		}
		select {
		case <-started:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if polls == 2 {
			return &api.ControllerCommand{
				Type:       api.ControllerCommandTypeRunAssignment,
				Assignment: &api.RunAssignment{ID: runID},
			}, nil
		}
		if polls < 6 {
			return &api.ControllerCommand{
				Type:   api.ControllerCommandTypeCancelRun,
				Cancel: &api.CancelRunCommand{ID: runID, JobNamespace: "slipramp-runs"},
			}, nil
		}
		cancel()

		return nil, ctx.Err()
	})

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if polls != 6 || ensureCalls.Load() != 1 {
		t.Fatalf("polls = %d, materializations = %d; want 6, 1", polls, ensureCalls.Load())
	}
}

func TestControllerCompletesRunInLifecycleOrder(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	handle := runjob.Handle{
		RunID:     runID,
		Namespace: "slipramp-runs",
		UID:       "job-uid",
	}
	succeeded := api.RunObservationOutcomeSucceeded
	exitCode := int32(0)
	observations := []runjob.Observation{
		{Phase: api.RunObservationPhaseRunning, JobUID: &handle.UID},
		{
			Phase:    api.RunObservationPhaseFinished,
			Outcome:  &succeeded,
			JobUID:   &handle.UID,
			ExitCode: &exitCode,
		},
	}
	var mutex sync.Mutex
	index := 0
	deleted := false
	jobs := &stubRunJobs{
		ensure: func(context.Context, api.RunAssignment) (runjob.Ensured, error) {
			t.Error("late assignment rematerialized the completed Run")

			return runjob.Ensured{}, errors.New("unexpected materialization")
		},
		observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
			mutex.Lock()
			defer mutex.Unlock()
			observation := observations[min(index, len(observations)-1)]
			index++

			return observation, nil
		},
		openLog: func(context.Context, runjob.Handle, bool) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("complete log\n")), nil
		},
		delete: func(context.Context, runjob.Handle) error {
			deleted = true

			return nil
		},
	}
	logs := &memoryLogReporter{}
	terminalReports := 0
	app := newTestController(jobs, logs)
	app.reports = runObservationReporterFunc(func(
		_ context.Context,
		_ uuid.UUID,
		observation api.RunObservation,
	) error {
		if observation.Phase != api.RunObservationPhaseFinished {
			return nil
		}
		terminalReports++
		if !logs.isSealed() {
			t.Error("terminal observation reported before log seal")
		}
		if deleted {
			t.Error("execution Job deleted before terminal observation")
		}
		if terminalReports == 1 {
			return errors.New("Server unavailable")
		}

		return nil
	})

	app.startRunWorker(context.Background(), handle, nil, nil)
	app.runWorkers.Wait()
	if err := app.handleAssignment(context.Background(), api.RunAssignment{ID: runID}); err != nil {
		t.Fatalf("late handleAssignment() error = %v", err)
	}
	app.runWorkers.Wait()

	if got := string(logs.bytes()); got != "complete log\n" {
		t.Errorf("durable log = %q", got)
	}
	if terminalReports != 2 {
		t.Errorf("terminal reports = %d, want 2", terminalReports)
	}
	if !deleted {
		t.Error("completed execution Job was not deleted")
	}
}

func TestControllerCompletesRunWhoseActionNeverStarted(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	handle := runjob.Handle{
		RunID:     runID,
		Namespace: "slipramp-runs",
		UID:       "job-uid",
	}
	failed := api.RunObservationOutcomeFailed
	deleted := false
	jobs := &stubRunJobs{
		observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
			return runjob.Observation{
				Phase:   api.RunObservationPhaseFinished,
				Outcome: &failed,
				JobUID:  &handle.UID,
			}, nil
		},
		delete: func(context.Context, runjob.Handle) error {
			deleted = true

			return nil
		},
	}
	logs := &memoryLogReporter{}
	app := newTestController(jobs, logs)

	app.startRunWorker(context.Background(), handle, nil, nil)
	app.runWorkers.Wait()

	if !logs.isSealed() || len(logs.bytes()) != 0 {
		t.Error("empty terminal log was not sealed")
	}
	if !deleted {
		t.Error("terminal Job was not deleted")
	}
}

func TestControllerCompletesTerminalRunAfterLogSourceDisappears(t *testing.T) {
	t.Parallel()

	for _, alreadyFailed := range []bool{false, true} {
		t.Run(fmt.Sprintf("alreadyFailed=%t", alreadyFailed), func(t *testing.T) {
			t.Parallel()

			runID := uuid.New()
			handle := runjob.Handle{
				RunID:     runID,
				Namespace: "slipramp-runs",
				UID:       "job-uid",
			}
			failed := api.RunObservationOutcomeFailed
			reason := "BackoffLimitExceeded"
			deleted := false
			jobs := &stubRunJobs{
				observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
					return runjob.Observation{
						Phase:   api.RunObservationPhaseFinished,
						Outcome: &failed,
						JobUID:  &handle.UID,
						Reason:  &reason,
					}, nil
				},
				delete: func(context.Context, runjob.Handle) error {
					deleted = true

					return nil
				},
			}
			logs := &memoryLogReporter{contents: []byte("durable prefix\n"), failed: alreadyFailed}
			var reported *api.RunObservation
			app := newTestController(jobs, logs)
			app.reports = runObservationReporterFunc(func(
				_ context.Context,
				_ uuid.UUID,
				observation api.RunObservation,
			) error {
				if !logs.isFailed() {
					t.Error("terminal observation reported before incomplete log was failed")
				}
				reported = &observation

				return nil
			})

			app.startRunWorker(context.Background(), handle, nil, nil)
			app.runWorkers.Wait()

			if reported == nil || reported.Outcome == nil || *reported.Outcome != failed ||
				reported.Reason == nil || *reported.Reason != reason {
				t.Fatalf("terminal observation = %#v", reported)
			}
			if !logs.isFailed() || logs.isSealed() {
				t.Error("incomplete log was not retained as failed")
			}
			if !deleted {
				t.Error("terminal Job was not deleted after failed-log recovery")
			}
		})
	}
}

func TestControllerCancellationDrainsLogBeforeReporting(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	handle := runjob.Handle{
		RunID:     runID,
		Namespace: "slipramp-runs",
		UID:       "job-uid",
	}
	reader, writer := io.Pipe()
	streamOpened := make(chan struct{})
	var openOnce sync.Once
	var cancellationRequested atomic.Bool
	canceled := api.RunObservationOutcomeCanceled
	jobs := &stubRunJobs{
		observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
			if cancellationRequested.Load() {
				return runjob.Observation{
					Phase:   api.RunObservationPhaseFinished,
					Outcome: &canceled,
					JobUID:  &handle.UID,
				}, nil
			}

			return runjob.Observation{
				Phase:  api.RunObservationPhaseRunning,
				JobUID: &handle.UID,
			}, nil
		},
		openLog: func(context.Context, runjob.Handle, bool) (io.ReadCloser, error) {
			openOnce.Do(func() { close(streamOpened) })

			return reader, nil
		},
		requestCancellation: func(_ context.Context, command api.CancelRunCommand) (runjob.CancellationResult, error) {
			if command.JobUID == nil || *command.JobUID != handle.UID {
				t.Error("cancellation did not retain the worker's pinned Job UID")
			}
			if _, err := io.WriteString(writer, "during shutdown\n"); err != nil {
				return runjob.CancellationResult{}, err
			}
			if err := writer.Close(); err != nil {
				return runjob.CancellationResult{}, err
			}
			cancellationRequested.Store(true)

			return runjob.CancellationResult{Handle: handle, JobRetained: true}, nil
		},
	}
	logs := &memoryLogReporter{}
	reported := make(chan api.RunObservation, 2)
	app := newTestController(jobs, logs)
	app.reports = runObservationReporterFunc(func(
		_ context.Context,
		_ uuid.UUID,
		observation api.RunObservation,
	) error {
		if observation.Phase == api.RunObservationPhaseFinished && !logs.isSealed() {
			t.Error("canceled Run reported before log seal")
		}
		reported <- observation

		return nil
	})

	observer := app.startRunWorker(context.Background(), handle, nil, nil)
	select {
	case <-streamOpened:
	case <-time.After(2 * time.Second):
		t.Fatal("controller did not open the live log stream")
	}

	if _, err := io.WriteString(writer, "before cancel\n"); err != nil {
		t.Fatalf("write pre-cancel log: %v", err)
	}
	command := api.CancelRunCommand{
		ID:           runID,
		JobNamespace: api.CatalogName(handle.Namespace),
	}
	if err := app.handleCancellation(context.Background(), command); err != nil {
		t.Fatalf("handleCancellation() error = %v", err)
	}

	select {
	case <-observer.done:
	case <-time.After(2 * time.Second):
		t.Fatal("controller did not finish cancellation")
	}
	if got := string(logs.bytes()); got != "before cancel\nduring shutdown\n" {
		t.Errorf("canceled Run log = %q", got)
	}

	var terminal api.RunObservation
	for len(reported) > 0 {
		observation := <-reported
		if observation.Phase == api.RunObservationPhaseFinished {
			terminal = observation
		}
	}
	if terminal.Outcome == nil ||
		*terminal.Outcome != api.RunObservationOutcomeCanceled {
		t.Fatalf("terminal observation = %#v", terminal)
	}
}

func TestControllerCancellationWinsBeforeMaterialization(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	observeCalls := 0
	jobs := &stubRunJobs{
		observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
			observeCalls++

			return runjob.Observation{}, nil
		},
		requestCancellation: func(_ context.Context, command api.CancelRunCommand) (runjob.CancellationResult, error) {
			return runjob.CancellationResult{Handle: runjob.Handle{
				RunID:     command.ID,
				Namespace: string(command.JobNamespace),
			}}, nil
		},
	}
	logs := &memoryLogReporter{}
	var reported api.RunObservation
	app := newTestController(jobs, logs)
	app.reports = runObservationReporterFunc(func(
		_ context.Context,
		_ uuid.UUID,
		observation api.RunObservation,
	) error {
		reported = observation

		return nil
	})

	err := app.handleCancellation(context.Background(), api.CancelRunCommand{
		ID:           runID,
		JobNamespace: "slipramp-runs",
	})
	if err != nil {
		t.Fatalf("handleCancellation() error = %v", err)
	}
	app.runWorkers.Wait()

	if observeCalls != 0 {
		t.Errorf("Kubernetes observations = %d, want 0", observeCalls)
	}
	if reported.Outcome == nil ||
		*reported.Outcome != api.RunObservationOutcomeCanceled {
		t.Fatalf("terminal observation = %#v", reported)
	}
	if !logs.isSealed() || len(logs.bytes()) != 0 {
		t.Errorf("empty canceled log was not sealed")
	}
}

func TestControllerCancellationWhileAssignmentAwaitsJobCreation(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	ensureCalls := 0
	jobs := &stubRunJobs{
		ensure: func(context.Context, api.RunAssignment) (runjob.Ensured, error) {
			ensureCalls++

			return runjob.Ensured{}, nil
		},
		requestCancellation: func(_ context.Context, command api.CancelRunCommand) (runjob.CancellationResult, error) {
			return runjob.CancellationResult{Handle: runjob.Handle{
				RunID: command.ID, Namespace: string(command.JobNamespace),
			}}, nil
		},
	}
	logs := &memoryLogReporter{}
	var reported api.RunObservation
	app := newTestController(jobs, logs)
	app.runSlots = make(chan struct{}, 1)
	app.runSlots <- struct{}{}
	app.reports = runObservationReporterFunc(func(
		_ context.Context,
		_ uuid.UUID,
		observation api.RunObservation,
	) error {
		reported = observation

		return nil
	})

	assignment := api.RunAssignment{ID: runID}
	assignment.Placement.JobNamespace = "slipramp-runs"
	worker := app.startRunWorker(
		context.Background(),
		runjob.Handle{RunID: runID, Namespace: "slipramp-runs"},
		&assignment,
		nil,
	)
	command := api.CancelRunCommand{ID: runID, JobNamespace: "slipramp-runs"}
	if err := app.handleCancellation(context.Background(), command); err != nil {
		t.Fatalf("first handleCancellation() error = %v", err)
	}
	if err := app.handleCancellation(context.Background(), command); err != nil {
		t.Fatalf("duplicate handleCancellation() error = %v", err)
	}
	select {
	case <-worker.done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not finish while assignment awaited Job creation")
	}
	if ensureCalls != 0 {
		t.Errorf("Job creation calls = %d, want 0", ensureCalls)
	}
	if reported.Outcome == nil || *reported.Outcome != api.RunObservationOutcomeCanceled {
		t.Errorf("terminal observation = %#v", reported)
	}
}

func TestControllerRetainsJobAfterPermanentLogRejection(t *testing.T) {
	t.Parallel()

	for _, status := range []int{400, 404, 409, 413} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()

			runID := uuid.New()
			handle := runjob.Handle{
				RunID:     runID,
				Namespace: "slipramp-runs",
				UID:       "job-uid",
			}
			succeeded := api.RunObservationOutcomeSucceeded
			deleteCalls := 0
			jobs := &stubRunJobs{
				observe: func(context.Context, runjob.Handle) (runjob.Observation, error) {
					return runjob.Observation{
						Phase:   api.RunObservationPhaseFinished,
						Outcome: &succeeded,
						JobUID:  &handle.UID,
					}, nil
				},
				openLog: func(context.Context, runjob.Handle, bool) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("orphan log\n")), nil
				},
				delete: func(context.Context, runjob.Handle) error {
					deleteCalls++

					return nil
				},
			}
			logs := &memoryLogReporter{cursorErrors: []error{
				newServerResponseError("get Run log cursor", status),
			}}
			app := newTestController(jobs, logs)

			app.startRunWorker(context.Background(), handle, nil, nil)
			app.runWorkers.Wait()

			if deleteCalls != 0 {
				t.Errorf("Job deletion calls = %d, want 0", deleteCalls)
			}
			app.reapRunWorkers()
			if app.activeRuns[runID] == nil {
				t.Error("blocked Run was released for repeated assignment")
			}
		})
	}
}

func newTestController(jobs runJobs, logs runLogReporter) *Controller {
	return &Controller{
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		enrollment:     controllerEnrollerFunc(func(context.Context) error { return nil }),
		server:         commandPollerFunc(func(context.Context) (*api.ControllerCommand, error) { return nil, nil }),
		reports:        runObservationReporterFunc(func(context.Context, uuid.UUID, api.RunObservation) error { return nil }),
		jobs:           jobs,
		logs:           logs,
		retryDelay:     time.Millisecond,
		observeEvery:   time.Millisecond,
		heartbeatEvery: time.Hour,
		now:            time.Now,
		activeRuns:     make(map[uuid.UUID]*runWorker),
	}
}
