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
	ensure              func(context.Context, api.RunAssignment) (ensuredRunJob, error)
	requestCancellation func(context.Context, api.CancelRunCommand) (cancellationResult, error)
	recover             func(context.Context) ([]jobHandle, error)
	observe             func(context.Context, jobHandle) (runObservation, error)
	openLog             func(context.Context, jobHandle, bool) (io.ReadCloser, error)
	delete              func(context.Context, jobHandle) error
}

func (s *stubRunJobs) Ensure(ctx context.Context, assignment api.RunAssignment) (ensuredRunJob, error) {
	if s.ensure == nil {
		return ensuredRunJob{}, nil
	}

	return s.ensure(ctx, assignment)
}

func (s *stubRunJobs) RequestCancellation(
	ctx context.Context,
	command api.CancelRunCommand,
) (cancellationResult, error) {
	return s.requestCancellation(ctx, command)
}

func (s *stubRunJobs) Recover(ctx context.Context) ([]jobHandle, error) {
	if s.recover == nil {
		return nil, nil
	}

	return s.recover(ctx)
}

func (s *stubRunJobs) Observe(ctx context.Context, handle jobHandle) (runObservation, error) {
	return s.observe(ctx, handle)
}

func (s *stubRunJobs) OpenLog(ctx context.Context, handle jobHandle, follow bool) (io.ReadCloser, error) {
	if s.openLog == nil {
		return nil, errLogNotReady
	}

	return s.openLog(ctx, handle, follow)
}

func (s *stubRunJobs) Delete(ctx context.Context, handle jobHandle) error {
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
	handle := jobHandle{runID: uuid.New(), namespace: "slipramp-runs", uid: "job-uid"}
	handle.name = runJobName(handle.runID)
	polling := make(chan struct{})
	jobs := &stubRunJobs{
		recover: func(context.Context) ([]jobHandle, error) { return []jobHandle{handle}, nil },
		observe: func(context.Context, jobHandle) (runObservation, error) {
			return runObservation{phase: api.RunObservationPhaseRunning, jobUID: &handle.uid}, nil
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
	jobs := &stubRunJobs{ensure: func(context.Context, api.RunAssignment) (ensuredRunJob, error) {
		return ensuredRunJob{}, permanentMaterialization(errors.New("invalid Job"))
	}}
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
	jobs := &stubRunJobs{ensure: func(ctx context.Context, _ api.RunAssignment) (ensuredRunJob, error) {
		if ensureCalls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()

		return ensuredRunJob{}, ctx.Err()
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
	handle := jobHandle{
		runID:     runID,
		namespace: "slipramp-runs",
		name:      runJobName(runID),
		uid:       "job-uid",
	}
	succeeded := api.RunObservationOutcomeSucceeded
	exitCode := int32(0)
	observations := []runObservation{
		{phase: api.RunObservationPhaseRunning, jobUID: &handle.uid},
		{
			phase:    api.RunObservationPhaseFinished,
			outcome:  &succeeded,
			jobUID:   &handle.uid,
			exitCode: &exitCode,
		},
	}
	var mutex sync.Mutex
	index := 0
	deleted := false
	jobs := &stubRunJobs{
		ensure: func(context.Context, api.RunAssignment) (ensuredRunJob, error) {
			t.Error("late assignment rematerialized the completed Run")

			return ensuredRunJob{}, errors.New("unexpected materialization")
		},
		observe: func(context.Context, jobHandle) (runObservation, error) {
			mutex.Lock()
			defer mutex.Unlock()
			observation := observations[min(index, len(observations)-1)]
			index++

			return observation, nil
		},
		openLog: func(context.Context, jobHandle, bool) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("complete log\n")), nil
		},
		delete: func(context.Context, jobHandle) error {
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
	handle := jobHandle{
		runID:     runID,
		namespace: "slipramp-runs",
		name:      runJobName(runID),
		uid:       "job-uid",
	}
	failed := api.RunObservationOutcomeFailed
	deleted := false
	jobs := &stubRunJobs{
		observe: func(context.Context, jobHandle) (runObservation, error) {
			return runObservation{
				phase:   api.RunObservationPhaseFinished,
				outcome: &failed,
				jobUID:  &handle.uid,
			}, nil
		},
		delete: func(context.Context, jobHandle) error {
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
			handle := jobHandle{
				runID:     runID,
				namespace: "slipramp-runs",
				name:      runJobName(runID),
				uid:       "job-uid",
			}
			failed := api.RunObservationOutcomeFailed
			reason := "BackoffLimitExceeded"
			deleted := false
			jobs := &stubRunJobs{
				observe: func(context.Context, jobHandle) (runObservation, error) {
					return runObservation{
						phase:   api.RunObservationPhaseFinished,
						outcome: &failed,
						jobUID:  &handle.uid,
						reason:  &reason,
					}, nil
				},
				delete: func(context.Context, jobHandle) error {
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
	handle := jobHandle{
		runID:     runID,
		namespace: "slipramp-runs",
		name:      runJobName(runID),
		uid:       "job-uid",
	}
	reader, writer := io.Pipe()
	streamOpened := make(chan struct{})
	var openOnce sync.Once
	var cancellationRequested atomic.Bool
	canceled := api.RunObservationOutcomeCanceled
	jobs := &stubRunJobs{
		observe: func(context.Context, jobHandle) (runObservation, error) {
			if cancellationRequested.Load() {
				return runObservation{
					phase:   api.RunObservationPhaseFinished,
					outcome: &canceled,
					jobUID:  &handle.uid,
				}, nil
			}

			return runObservation{
				phase:  api.RunObservationPhaseRunning,
				jobUID: &handle.uid,
			}, nil
		},
		openLog: func(context.Context, jobHandle, bool) (io.ReadCloser, error) {
			openOnce.Do(func() { close(streamOpened) })

			return reader, nil
		},
		requestCancellation: func(_ context.Context, command api.CancelRunCommand) (cancellationResult, error) {
			if command.JobUID == nil || *command.JobUID != handle.uid {
				t.Error("cancellation did not retain the worker's pinned Job UID")
			}
			if _, err := io.WriteString(writer, "during shutdown\n"); err != nil {
				return cancellationResult{}, err
			}
			if err := writer.Close(); err != nil {
				return cancellationResult{}, err
			}
			cancellationRequested.Store(true)

			return cancellationResult{handle: handle, jobRetained: true}, nil
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
		JobNamespace: api.CatalogName(handle.namespace),
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
		observe: func(context.Context, jobHandle) (runObservation, error) {
			observeCalls++

			return runObservation{}, nil
		},
		requestCancellation: func(_ context.Context, command api.CancelRunCommand) (cancellationResult, error) {
			return cancellationResult{handle: jobHandle{
				runID:     command.ID,
				namespace: string(command.JobNamespace),
				name:      runJobName(command.ID),
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

func TestControllerRetainsJobAfterPermanentLogRejection(t *testing.T) {
	t.Parallel()

	for _, status := range []int{400, 404, 409, 413} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()

			runID := uuid.New()
			handle := jobHandle{
				runID:     runID,
				namespace: "slipramp-runs",
				name:      runJobName(runID),
				uid:       "job-uid",
			}
			succeeded := api.RunObservationOutcomeSucceeded
			deleteCalls := 0
			jobs := &stubRunJobs{
				observe: func(context.Context, jobHandle) (runObservation, error) {
					return runObservation{
						phase:   api.RunObservationPhaseFinished,
						outcome: &succeeded,
						jobUID:  &handle.uid,
					}, nil
				},
				openLog: func(context.Context, jobHandle, bool) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("orphan log\n")), nil
				},
				delete: func(context.Context, jobHandle) error {
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
