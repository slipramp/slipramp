package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestCancelRunRecordsIntentInUTC(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	want := Run{ID: runID, Phase: PhaseAssigned}
	runs := &recordingCancellations{canceled: want}
	service := &Service{cancellations: runs}
	location := time.FixedZone("test", -6*60*60)
	requestedAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, location)
	service.now = func() time.Time { return requestedAt }
	actor := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
	}

	record, err := service.CancelRun(context.Background(), CancelRequest{RunID: runID, Actor: &actor})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if record.ID != want.ID || runs.cancelID != runID {
		t.Errorf("canceled Run = %#v, store ID = %s", record, runs.cancelID)
	}
	if !runs.cancelAt.Equal(requestedAt) || runs.cancelAt.Location() != time.UTC {
		t.Errorf("requested at = %s, want %s in UTC", runs.cancelAt, requestedAt)
	}
	if runs.canceledBy == nil || runs.canceledBy.PrincipalID != actor.ID ||
		runs.canceledBy.DisplayName != actor.DisplayName {
		t.Errorf("cancellation actor = %#v", runs.canceledBy)
	}
}

func TestCancelRunMapsExpectedFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		runID uuid.UUID
		err   error
		want  error
		calls int
	}{
		{name: "missing ID", want: ErrInvalidRequest},
		{name: "missing Run", runID: uuid.New(), err: ErrRunNotFound, want: ErrRunNotFound, calls: 1},
		{name: "finished Run", runID: uuid.New(), err: ErrRunFinished, want: ErrRunFinished, calls: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runs := &recordingCancellations{cancelError: test.err}
			service := &Service{cancellations: runs, now: time.Now}
			_, err := service.CancelRun(context.Background(), CancelRequest{RunID: test.runID})
			if !errors.Is(err, test.want) {
				t.Fatalf("CancelRun() error = %v, want %v", err, test.want)
			}
			if runs.cancelCalls != test.calls {
				t.Errorf("store calls = %d, want %d", runs.cancelCalls, test.calls)
			}
		})
	}
}

func TestNextRunToCancelReturnsPendingRun(t *testing.T) {
	t.Parallel()

	want := Run{ID: uuid.New(), Phase: PhaseRunning}
	runs := &recordingCancellations{nextCancel: want}
	service := &Service{cancellations: runs}

	record, err := service.NextRunToCancel(context.Background(), "colo")
	if err != nil {
		t.Fatalf("NextRunToCancel() error = %v", err)
	}
	if record.ID != want.ID || runs.nextCancelCluster != "colo" {
		t.Errorf("Run to cancel = %#v, cluster = %q", record, runs.nextCancelCluster)
	}
}

func TestNextRunToCancelRejectsBlankClusterName(t *testing.T) {
	t.Parallel()

	runs := &recordingCancellations{}
	service := &Service{cancellations: runs}
	_, err := service.NextRunToCancel(context.Background(), " ")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NextRunToCancel() error = %v, want %v", err, ErrInvalidRequest)
	}
	if runs.nextCancelCalls != 0 {
		t.Errorf("store calls = %d, want 0", runs.nextCancelCalls)
	}
}

func TestCancellationState(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	requestedAt := createdAt.Add(time.Minute)
	finishedAt := requestedAt.Add(time.Minute)
	sameTime := requestedAt
	succeeded := OutcomeSucceeded
	failed := OutcomeFailed
	canceled := OutcomeCanceled
	timedOut := OutcomeTimedOut
	cancelRequested := ReasonCancelRequested
	superseded := ReasonSupersededByGitChange
	reviewRejected := ReasonReviewRejected
	unknownReason := "UnknownCancellation"
	zero := time.Time{}
	beforeCreation := createdAt.Add(-time.Nanosecond)
	afterFinish := finishedAt.Add(time.Nanosecond)

	type testCase struct {
		name    string
		status  CancellationStatus
		want    CancellationState
		wantErr bool
	}
	tests := []testCase{
		{
			name:   "queued without request",
			status: CancellationStatus{Phase: PhaseQueued, CreatedAt: createdAt},
			want:   CancellationStateNotRequested,
		},
		{
			name:   "active before request",
			status: CancellationStatus{Phase: PhaseRunning, CreatedAt: createdAt},
			want:   CancellationStateNotRequested,
		},
		{
			name: "active after request",
			status: CancellationStatus{
				Phase: PhaseRunning, CreatedAt: createdAt, CancellationRequestedAt: &requestedAt,
			},
			want: CancellationStateRequested,
		},
		{
			name: "immediate queued cancellation",
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &canceled, OutcomeReason: &cancelRequested,
				CreatedAt: createdAt, FinishedAt: &sameTime, CancellationRequestedAt: &sameTime,
			},
			want: CancellationStateCompleted,
		},
		{
			name: "requested cancellation completed",
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &canceled, OutcomeReason: &cancelRequested,
				CreatedAt: createdAt, FinishedAt: &finishedAt, CancellationRequestedAt: &requestedAt,
			},
			want: CancellationStateCompleted,
		},
		{
			name: "Git supersession",
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &canceled, OutcomeReason: &superseded,
				CreatedAt: createdAt, FinishedAt: &finishedAt,
			},
			want: CancellationStateNotRequested,
		},
		{
			name: "review rejection",
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &canceled, OutcomeReason: &reviewRejected,
				CreatedAt: createdAt, FinishedAt: &finishedAt,
			},
			want: CancellationStateNotRequested,
		},
		{
			name:    "invalid phase",
			status:  CancellationStatus{Phase: "canceling", CreatedAt: createdAt},
			wantErr: true,
		},
		{
			name: "unfinished outcome",
			status: CancellationStatus{
				Phase: PhaseRunning, Outcome: &succeeded, CreatedAt: createdAt,
			},
			wantErr: true,
		},
		{
			name: "finished without outcome",
			status: CancellationStatus{
				Phase: PhaseFinished, CreatedAt: createdAt, FinishedAt: &finishedAt,
			},
			wantErr: true,
		},
		{
			name: "unknown unrequested cancellation reason",
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &canceled, OutcomeReason: &unknownReason,
				CreatedAt: createdAt, FinishedAt: &finishedAt,
			},
			wantErr: true,
		},
		{
			name: "requested cancellation with non-operator reason",
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &canceled, OutcomeReason: &superseded,
				CreatedAt: createdAt, FinishedAt: &finishedAt, CancellationRequestedAt: &requestedAt,
			},
			wantErr: true,
		},
		{
			name: "queued request not atomically finished",
			status: CancellationStatus{
				Phase: PhaseQueued, CreatedAt: createdAt, CancellationRequestedAt: &requestedAt,
			},
			wantErr: true,
		},
		{
			name: "zero request time",
			status: CancellationStatus{
				Phase: PhaseRunning, CreatedAt: createdAt, CancellationRequestedAt: &zero,
			},
			wantErr: true,
		},
		{
			name: "request before creation",
			status: CancellationStatus{
				Phase: PhaseRunning, CreatedAt: createdAt, CancellationRequestedAt: &beforeCreation,
			},
			wantErr: true,
		},
		{
			name: "request after terminal observation",
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &succeeded, CreatedAt: createdAt,
				FinishedAt: &finishedAt, CancellationRequestedAt: &afterFinish,
			},
			wantErr: true,
		},
		{
			name: "request and terminal observation at same time",
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &succeeded, CreatedAt: createdAt,
				FinishedAt: &sameTime, CancellationRequestedAt: &sameTime,
			},
			want: CancellationStateOvertaken,
		},
		{
			name: "canceler without request",
			status: CancellationStatus{
				Phase: PhaseRunning, CreatedAt: createdAt, CanceledByPresent: true,
			},
			wantErr: true,
		},
	}
	for _, outcome := range []Outcome{succeeded, failed, timedOut} {
		tests = append(tests, testCase{
			name: "request overtaken by " + string(outcome),
			status: CancellationStatus{
				Phase: PhaseFinished, Outcome: &outcome, CreatedAt: createdAt,
				FinishedAt: &finishedAt, CancellationRequestedAt: &requestedAt,
			},
			want: CancellationStateOvertaken,
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			state, err := test.status.CancellationState()
			if test.wantErr {
				if !errors.Is(err, ErrInvalidCancellation) {
					t.Fatalf("CancellationState() error = %v, want invalid cancellation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CancellationState() error = %v", err)
			}
			if state != test.want {
				t.Errorf("CancellationState() = %q, want %q", state, test.want)
			}
		})
	}
}

func TestPlanCancellationRequest(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	requestedAt := createdAt.Add(time.Minute)
	finishedAt := requestedAt.Add(time.Minute)
	canceled := OutcomeCanceled
	cancelReason := ReasonCancelRequested
	succeeded := OutcomeSucceeded

	tests := []struct {
		name      string
		current   CancellationStatus
		wantFrom  CancellationState
		wantTo    CancellationState
		operation CancellationOperation
		wantErr   error
	}{
		{
			name:     "queued cancellation",
			current:  CancellationStatus{Phase: PhaseQueued, CreatedAt: createdAt},
			wantFrom: CancellationStateNotRequested, wantTo: CancellationStateCompleted,
			operation: CancellationOperationFinishQueued,
		},
		{
			name:     "active cancellation",
			current:  CancellationStatus{Phase: PhaseRunning, CreatedAt: createdAt},
			wantFrom: CancellationStateNotRequested, wantTo: CancellationStateRequested,
			operation: CancellationOperationRequestActive,
		},
		{
			name: "repeated active cancellation",
			current: CancellationStatus{
				Phase: PhaseRunning, CreatedAt: createdAt, CancellationRequestedAt: &requestedAt,
			},
			wantFrom: CancellationStateRequested, wantTo: CancellationStateRequested,
			operation: CancellationOperationNone,
		},
		{
			name: "repeated completed cancellation",
			current: CancellationStatus{
				Phase: PhaseFinished, Outcome: &canceled, OutcomeReason: &cancelReason,
				CreatedAt: createdAt, FinishedAt: &finishedAt, CancellationRequestedAt: &requestedAt,
			},
			wantFrom: CancellationStateCompleted, wantTo: CancellationStateCompleted,
			operation: CancellationOperationNone,
		},
		{
			name: "repeated overtaken cancellation",
			current: CancellationStatus{
				Phase: PhaseFinished, Outcome: &succeeded, CreatedAt: createdAt,
				FinishedAt: &finishedAt, CancellationRequestedAt: &requestedAt,
			},
			wantFrom: CancellationStateOvertaken, wantTo: CancellationStateOvertaken,
			operation: CancellationOperationNone,
		},
		{
			name: "new request after normal completion",
			current: CancellationStatus{
				Phase: PhaseFinished, Outcome: &succeeded, CreatedAt: createdAt, FinishedAt: &finishedAt,
			},
			wantErr: ErrRunFinished,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transition, err := PlanCancellationRequest(test.current, requestedAt.Add(time.Hour))
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("PlanCancellationRequest() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanCancellationRequest() error = %v", err)
			}
			if transition.From != test.wantFrom || transition.To != test.wantTo ||
				transition.Operation != test.operation {
				t.Errorf("PlanCancellationRequest() = %#v", transition)
			}
		})
	}
}

type recordingCancellations struct {
	cancelCalls       int
	cancelID          uuid.UUID
	cancelAt          time.Time
	canceledBy        *productidentity.ActorSnapshot
	canceled          Run
	cancelError       error
	nextCancelCalls   int
	nextCancelCluster string
	nextCancel        Run
	nextCancelError   error
}

func (s *recordingCancellations) CancelRun(
	_ context.Context,
	runID uuid.UUID,
	requestedAt time.Time,
	canceledBy *productidentity.ActorSnapshot,
) (Run, error) {
	s.cancelCalls++
	s.cancelID = runID
	s.cancelAt = requestedAt
	s.canceledBy = canceledBy

	return s.canceled, s.cancelError
}

func (s *recordingCancellations) GetRunToCancelForCluster(
	_ context.Context,
	clusterName string,
) (Run, error) {
	s.nextCancelCalls++
	s.nextCancelCluster = clusterName

	return s.nextCancel, s.nextCancelError
}
