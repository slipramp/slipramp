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
	ensureError  error
	observations []runObservation
	index        int
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

	err = app.Run(context.Background())
	if !errors.Is(err, errControllerCredentialRejected) {
		t.Fatalf("Run() error = %v, want %v", err, errControllerCredentialRejected)
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
		observedRuns:   make(map[uuid.UUID]struct{}),
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
		Assignment: api.RunAssignment{ID: runID},
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
