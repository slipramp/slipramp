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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/testcheck"
)

type commandPollerFunc func(ctx context.Context) (*api.ControllerCommand, error)

func (f commandPollerFunc) Poll(ctx context.Context) (*api.ControllerCommand, error) {
	return f(ctx)
}

type controllerEnrollerFunc func(ctx context.Context) error

func (f controllerEnrollerFunc) Enroll(ctx context.Context) error {
	return f(ctx)
}

type runObservationReporterFunc func(
	ctx context.Context,
	runID uuid.UUID,
	observation api.RunObservation,
) error

func (f runObservationReporterFunc) ReportObservation(
	ctx context.Context,
	runID uuid.UUID,
	observation api.RunObservation,
) error {
	return f(ctx, runID, observation)
}

type scriptedRunJobs struct {
	ensureError    error
	cancelError    error
	cancelCommand  api.CancelRunCommand
	canceledJobUID *string
	observations   []runObservation
	index          int
}

func (s *scriptedRunJobs) Cancel(
	_ context.Context,
	command api.CancelRunCommand,
) (*string, error) {
	s.cancelCommand = command

	return s.canceledJobUID, s.cancelError
}

func (s *scriptedRunJobs) Ensure(
	context.Context,
	api.RunAssignment,
) (ensuredRunJob, error) {
	return ensuredRunJob{}, s.ensureError
}

func (*scriptedRunJobs) Recover(
	context.Context,
) ([]jobHandle, error) {
	return nil, nil
}

func (s *scriptedRunJobs) Observe(
	context.Context,
	jobHandle,
) (runObservation, error) {
	observation := s.observations[s.index]
	s.index++

	return observation, nil
}

func TestNewRequiresDependencies(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name       string
		logger     *slog.Logger
		kubernetes kubernetes.Interface
		want       string
	}{
		{
			name:       "logger",
			kubernetes: fake.NewClientset(),
			want:       "logger",
		},
		{
			name:   "Kubernetes client",
			logger: logger,
			want:   "kubernetes client",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := New(validConfig(), test.logger, test.kubernetes)
			testcheck.ErrorContains(t, err, test.want)
		})
	}
}

func TestControllerStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	app, err := New(validConfig(), logger, fake.NewClientset())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- app.Run(ctx) }()
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
	if !strings.Contains(output, "controller started") ||
		!strings.Contains(output, "controller stopped") {
		t.Errorf("logs = %q, want lifecycle messages", output)
	}
	if strings.Contains(output, validConfig().BearerToken) {
		t.Errorf("logs contain bearer token: %q", output)
	}
}

func TestControllerStopsAfterAuthenticationIsRejected(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(validConfig(), logger, fake.NewClientset())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	app.server = commandPollerFunc(func(context.Context) (*api.ControllerCommand, error) {
		return nil, errControllerCredentialRejected
	})
	app.enrollment = controllerEnrollerFunc(func(context.Context) error {
		return nil
	})

	err = app.Run(context.Background())
	if !errors.Is(err, errControllerCredentialRejected) {
		t.Fatalf("Run() error = %v, want %v", err, errControllerCredentialRejected)
	}
}

func TestControllerStopsAfterEnrollmentIsRejected(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(validConfig(), logger, fake.NewClientset())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	app.enrollment = controllerEnrollerFunc(func(context.Context) error {
		return errControllerEnrollmentCredentialRejected
	})

	err = app.Run(context.Background())
	if !errors.Is(err, errControllerEnrollmentCredentialRejected) {
		t.Fatalf(
			"Run() error = %v, want %v",
			err,
			errControllerEnrollmentCredentialRejected,
		)
	}
}

func TestControllerRecoversRunBeforePollingAfterRestart(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	finishJob := make(chan struct{})
	jobs := &recoveryRunJobs{
		handle: jobHandle{
			runID:     runID,
			namespace: "slipramp-runs",
			name:      runJobName(runID),
			uid:       "job-uid",
		},
		finish:   finishJob,
		contents: []byte("already durable\n"),
	}
	logs := &memoryLogReporter{contents: []byte("already ")}
	pollStarted := make(chan int, 1)
	reported := make(chan api.RunObservation, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &Controller{
		clusterName: "colo",
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		enrollment: controllerEnrollerFunc(func(context.Context) error {
			return nil
		}),
		server: commandPollerFunc(func(ctx context.Context) (*api.ControllerCommand, error) {
			pollStarted <- jobs.recoverCalls
			<-ctx.Done()

			return nil, ctx.Err()
		}),
		reports: runObservationReporterFunc(func(
			_ context.Context,
			gotRunID uuid.UUID,
			observation api.RunObservation,
		) error {
			if gotRunID != runID {
				t.Errorf("reported Run ID = %s, want %s", gotRunID, runID)
			}
			reported <- observation
			if observation.Phase == api.RunObservationPhaseFinished {
				cancel()
			}

			return nil
		}),
		jobs:           jobs,
		logs:           logs,
		retryDelay:     time.Millisecond,
		observeEvery:   time.Millisecond,
		heartbeatEvery: time.Hour,
		observedRuns:   make(map[uuid.UUID]*activeObserver),
	}

	result := make(chan error, 1)
	go func() { result <- app.Run(ctx) }()

	select {
	case recoverCalls := <-pollStarted:
		if recoverCalls != 1 {
			t.Errorf("poll began after %d recovery calls, want 1", recoverCalls)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("controller did not begin polling after recovery")
	}

	first := receiveObservation(t, reported)
	if first.Phase != api.RunObservationPhaseRunning {
		t.Fatalf("first recovered phase = %q, want running", first.Phase)
	}

	close(finishJob)
	second := receiveObservation(t, reported)
	if second.Phase != api.RunObservationPhaseFinished ||
		second.Outcome == nil ||
		*second.Outcome != api.RunObservationOutcomeSucceeded {
		t.Fatalf("final recovered observation = %#v", second)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("controller did not stop after the recovered Run finished")
	}

	if jobs.ensureCalls != 0 {
		t.Errorf("assignment materializations = %d, want 0", jobs.ensureCalls)
	}
	if logs.appendCalls != 1 || len(logs.appendOffsets) != 1 || logs.appendOffsets[0] != 8 {
		t.Errorf(
			"log appends = %d at %v, want one at durable offset 8",
			logs.appendCalls,
			logs.appendOffsets,
		)
	}
	if string(logs.contents) != "already durable\n" || !logs.sealed {
		t.Errorf("recovered log = %q, sealed = %t", logs.contents, logs.sealed)
	}
}

func TestControllerReportsChangedObservationsThroughCompletion(t *testing.T) {
	t.Parallel()

	observations := make(chan api.RunObservation, 3)
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	succeeded := api.RunObservationOutcomeSucceeded
	app := &Controller{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		reports: runObservationReporterFunc(func(
			_ context.Context,
			_ uuid.UUID,
			observation api.RunObservation,
		) error {
			observations <- observation

			return nil
		}),
		jobs: &scriptedRunJobs{
			observations: []runObservation{
				{phase: api.RunObservationPhasePreparing},
				{phase: api.RunObservationPhasePreparing},
				{phase: api.RunObservationPhaseRunning},
				{phase: api.RunObservationPhaseFinished, outcome: &succeeded},
			},
		},
		observeEvery:   time.Millisecond,
		heartbeatEvery: time.Hour,
		observedRuns:   make(map[uuid.UUID]*activeObserver),
	}

	app.startObservation(context.Background(), jobHandle{runID: runID})
	app.observers.Wait()
	close(observations)

	got := make([]api.RunObservationPhase, 0, len(observations))
	for observation := range observations {
		got = append(got, observation.Phase)
	}
	want := []api.RunObservationPhase{
		api.RunObservationPhasePreparing,
		api.RunObservationPhaseRunning,
		api.RunObservationPhaseFinished,
	}
	if len(got) != len(want) {
		t.Fatalf("reported phases = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("reported phases = %v, want %v", got, want)
		}
	}
}

func receiveObservation(
	t *testing.T,
	reported <-chan api.RunObservation,
) api.RunObservation {
	t.Helper()

	select {
	case observation := <-reported:
		return observation
	case <-time.After(2 * time.Second):
		t.Fatal("controller did not report a recovered observation")

		return api.RunObservation{}
	}
}

type recoveryRunJobs struct {
	handle       jobHandle
	finish       <-chan struct{}
	contents     []byte
	recoverCalls int
	ensureCalls  int
	observeCalls int
}

func (r *recoveryRunJobs) Ensure(
	context.Context,
	api.RunAssignment,
) (ensuredRunJob, error) {
	r.ensureCalls++

	return ensuredRunJob{}, nil
}

func (*recoveryRunJobs) Cancel(
	context.Context,
	api.CancelRunCommand,
) (*string, error) {
	return nil, nil
}

func (r *recoveryRunJobs) Recover(context.Context) ([]jobHandle, error) {
	r.recoverCalls++

	return []jobHandle{r.handle}, nil
}

func (r *recoveryRunJobs) Observe(
	ctx context.Context,
	_ jobHandle,
) (runObservation, error) {
	r.observeCalls++
	if r.observeCalls == 1 {
		jobUID := r.handle.uid

		return runObservation{
			phase:  api.RunObservationPhaseRunning,
			jobUID: &jobUID,
		}, nil
	}

	select {
	case <-ctx.Done():
		return runObservation{}, ctx.Err()
	case <-r.finish:
	}

	jobUID := r.handle.uid
	succeeded := api.RunObservationOutcomeSucceeded
	exitCode := int32(0)

	return runObservation{
		phase:    api.RunObservationPhaseFinished,
		outcome:  &succeeded,
		jobUID:   &jobUID,
		exitCode: &exitCode,
	}, nil
}

func (r *recoveryRunJobs) ReadLog(
	context.Context,
	jobHandle,
) ([]byte, error) {
	return append([]byte(nil), r.contents...), nil
}

func TestControllerReportsPermanentMaterializationFailure(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	var reported api.RunObservation
	app := &Controller{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		reports: runObservationReporterFunc(func(
			_ context.Context,
			_ uuid.UUID,
			observation api.RunObservation,
		) error {
			reported = observation

			return nil
		}),
		jobs: &scriptedRunJobs{
			ensureError: permanentMaterialization(errors.New("invalid Job")),
		},
	}

	err := app.handleCommand(context.Background(), api.ControllerCommand{
		Type:       api.ControllerCommandTypeRunAssignment,
		Assignment: &api.RunAssignment{ID: runID},
	})
	if err != nil {
		t.Fatalf("handleCommand() error = %v", err)
	}
	if reported.Phase != api.RunObservationPhaseFinished ||
		reported.Outcome == nil ||
		*reported.Outcome != api.RunObservationOutcomeFailed ||
		reported.Reason == nil || *reported.Reason != "MaterializationFailed" {
		t.Fatalf("reported observation = %#v", reported)
	}
}

func TestControllerCancelsRunAndReportsOutcome(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	jobUID := "job-uid"
	jobs := &scriptedRunJobs{canceledJobUID: &jobUID}
	var reported api.RunObservation
	app := &Controller{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		reports: runObservationReporterFunc(func(
			_ context.Context,
			_ uuid.UUID,
			observation api.RunObservation,
		) error {
			reported = observation

			return nil
		}),
		jobs:         jobs,
		observedRuns: make(map[uuid.UUID]*activeObserver),
	}
	command := api.CancelRunCommand{
		ID:           runID,
		JobNamespace: "slipramp-runs",
		JobUID:       &jobUID,
	}

	err := app.handleCommand(context.Background(), api.ControllerCommand{
		Type:   api.ControllerCommandTypeCancelRun,
		Cancel: &command,
	})
	if err != nil {
		t.Fatalf("handleCommand() error = %v", err)
	}
	if jobs.cancelCommand.ID != runID {
		t.Errorf("cancel command = %#v", jobs.cancelCommand)
	}
	if reported.Phase != api.RunObservationPhaseFinished ||
		reported.Outcome == nil ||
		*reported.Outcome != api.RunObservationOutcomeCanceled ||
		reported.JobUID == nil || *reported.JobUID != jobUID ||
		reported.Reason == nil || *reported.Reason != "CancelRequested" {
		t.Errorf("reported observation = %#v", reported)
	}
}
