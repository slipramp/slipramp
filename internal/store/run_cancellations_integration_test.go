package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/run"
)

func TestRunCancellationRaceWithAssignmentIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	record := singleCancellationTestRun(t, ctx, database)
	at := record.CreatedAt.Add(time.Minute)
	start := make(chan struct{})
	type cancelResult struct {
		record run.Run
		err    error
	}
	type claimResult struct {
		record run.Run
		err    error
	}
	canceled := make(chan cancelResult, 1)
	claimed := make(chan claimResult, 1)
	go func() {
		<-start
		result, err := database.CancelRun(ctx, record.ID, at, nil)
		canceled <- cancelResult{record: result, err: err}
	}()
	go func() {
		<-start
		result, err := database.ClaimQueuedRun(ctx, "colo", testSchedulingCapacity(), at)
		claimed <- claimResult{record: result, err: err}
	}()
	close(start)

	cancel := <-canceled
	claim := <-claimed
	if cancel.err != nil {
		t.Fatalf("CancelRun() error = %v", cancel.err)
	}
	stored, err := database.GetRun(ctx, record.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	state, err := stored.CancellationState()
	if err != nil {
		t.Fatalf("CancellationState() error = %v", err)
	}
	switch {
	case errors.Is(claim.err, run.ErrNoRunAssignment):
		if state != run.CancellationStateCompleted || stored.Phase != run.PhaseFinished {
			t.Errorf("cancel-won Run = %#v, state %q", stored, state)
		}
	case claim.err == nil:
		if claim.record.ID != record.ID || state != run.CancellationStateRequested ||
			stored.Phase != run.PhaseAssigned {
			t.Errorf("assignment-won Run = %#v, state %q", stored, state)
		}
	default:
		t.Fatalf("ClaimQueuedRun() error = %v", claim.err)
	}
}

func TestConcurrentRunCancellationRequestsIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	record := singleCancellationTestRun(t, ctx, database)
	assignedAt := record.CreatedAt.Add(time.Minute)
	if _, err := database.ClaimQueuedRun(ctx, "colo", testSchedulingCapacity(), assignedAt); err != nil {
		t.Fatalf("claim Run: %v", err)
	}

	const requestCount = 8
	start := make(chan struct{})
	type cancelResult struct {
		record run.Run
		err    error
	}
	results := make(chan cancelResult, requestCount)
	var workers sync.WaitGroup
	for index := range requestCount {
		workers.Go(func() {
			<-start
			result, err := database.CancelRun(
				ctx,
				record.ID,
				assignedAt.Add(time.Duration(index+1)*time.Second),
				nil,
			)
			results <- cancelResult{record: result, err: err}
		})
	}
	close(start)
	workers.Wait()
	close(results)

	var requestedAt *time.Time
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent CancelRun() error = %v", result.err)
		}
		if result.record.CancellationRequestedAt == nil {
			t.Fatal("concurrent cancellation did not persist request time")
		}
		if requestedAt == nil {
			requestedAt = result.record.CancellationRequestedAt
		} else if !requestedAt.Equal(*result.record.CancellationRequestedAt) {
			t.Errorf("concurrent request times differ: %s and %s",
				*requestedAt, *result.record.CancellationRequestedAt)
		}
	}

}

func TestRunCancellationRaceWithTerminalObservationIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	record := singleCancellationTestRun(t, ctx, database)
	assignedAt := record.CreatedAt.Add(time.Minute)
	if _, err := database.ClaimQueuedRun(ctx, "colo", testSchedulingCapacity(), assignedAt); err != nil {
		t.Fatalf("claim Run: %v", err)
	}

	at := assignedAt.Add(time.Minute)
	succeeded := run.OutcomeSucceeded
	start := make(chan struct{})
	cancelResult := make(chan error, 1)
	observationResult := make(chan error, 1)
	go func() {
		<-start
		_, err := database.CancelRun(ctx, record.ID, at, nil)
		cancelResult <- err
	}()
	go func() {
		<-start
		observationResult <- database.ApplyRunObservation(ctx, run.Observation{
			ID: record.ID, ClusterName: "colo", Phase: run.PhaseFinished,
			Outcome: &succeeded, ObservedAt: at,
		})
	}()
	close(start)

	cancelErr := <-cancelResult
	if err := <-observationResult; err != nil {
		t.Fatalf("ApplyRunObservation() error = %v", err)
	}
	stored, err := database.GetRun(ctx, record.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	state, err := stored.CancellationState()
	if err != nil {
		t.Fatalf("CancellationState() error = %v", err)
	}
	if stored.Outcome == nil || *stored.Outcome != run.OutcomeSucceeded {
		t.Fatalf("terminal Run = %#v", stored)
	}
	if cancelErr == nil {
		if state != run.CancellationStateOvertaken {
			t.Errorf("cancellation state = %q, want overtaken", state)
		}
	} else if !errors.Is(cancelErr, run.ErrRunFinished) || state != run.CancellationStateNotRequested {
		t.Errorf("cancellation race = error %v, state %q", cancelErr, state)
	}
	if _, err := database.GetRunToCancelForCluster(ctx, "colo"); !errors.Is(err, run.ErrNoRunToCancel) {
		t.Errorf("terminal Run cancellation command error = %v, want no command", err)
	}
}

func TestQueuedRunCancellationIsAtomicAndIdempotentIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	record := singleCancellationTestRun(t, ctx, database)
	at := record.CreatedAt.Add(time.Minute)
	first, err := database.CancelRun(ctx, record.ID, at, nil)
	if err != nil {
		t.Fatalf("first CancelRun() error = %v", err)
	}
	second, err := database.CancelRun(ctx, record.ID, at.Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("repeated CancelRun() error = %v", err)
	}
	if first.CancellationRequestedAt == nil || second.CancellationRequestedAt == nil ||
		!first.CancellationRequestedAt.Equal(*second.CancellationRequestedAt) {
		t.Errorf("queued cancellation request times = %v and %v", first.CancellationRequestedAt,
			second.CancellationRequestedAt)
	}
	state, err := first.CancellationState()
	if err != nil {
		t.Fatalf("CancellationState() error = %v", err)
	}
	if state != run.CancellationStateCompleted || first.Phase != run.PhaseFinished ||
		first.FinishedAt == nil || !first.FinishedAt.Equal(at) {
		t.Errorf("queued cancellation = %#v, state %q", first, state)
	}
	if _, err := database.GetRunToCancelForCluster(ctx, "colo"); !errors.Is(err, run.ErrNoRunToCancel) {
		t.Errorf("queued cancellation command error = %v, want no command", err)
	}
}

func TestActiveRunCancellationSurvivesDatabaseReopenIntegration(t *testing.T) {
	ctx, cfg, database := openIntegrationDatabase(t)

	record := singleCancellationTestRun(t, ctx, database)
	assignedAt := record.CreatedAt.Add(time.Minute)
	assigned, err := database.ClaimQueuedRun(ctx, "colo", testSchedulingCapacity(), assignedAt)
	if err != nil {
		t.Fatalf("claim Run: %v", err)
	}
	canceled := run.OutcomeCanceled
	if err := database.ApplyRunObservation(ctx, run.Observation{
		ID: record.ID, ClusterName: "colo", Phase: run.PhaseFinished,
		Outcome: &canceled, ObservedAt: assignedAt.Add(time.Minute),
	}); !errors.Is(err, run.ErrObservationConflict) {
		t.Fatalf("unrequested canceled observation error = %v, want conflict", err)
	}

	requestedAt := assignedAt.Add(time.Minute)
	requested, err := database.CancelRun(ctx, record.ID, requestedAt, nil)
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	repeated, err := database.CancelRun(ctx, record.ID, requestedAt.Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("repeated CancelRun() error = %v", err)
	}
	if requested.CancellationRequestedAt == nil || repeated.CancellationRequestedAt == nil ||
		!requested.CancellationRequestedAt.Equal(*repeated.CancellationRequestedAt) {
		t.Errorf("active cancellation request times = %v and %v",
			requested.CancellationRequestedAt, repeated.CancellationRequestedAt)
	}
	database.Close()

	reopened, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen Database: %v", err)
	}
	defer reopened.Close()
	pending, err := reopened.GetRunToCancelForCluster(ctx, "colo")
	if err != nil {
		t.Fatalf("GetRunToCancelForCluster() error = %v", err)
	}
	if pending.ID != record.ID || pending.Phase != run.PhaseAssigned ||
		pending.CancellationRequestedAt == nil || !pending.CancellationRequestedAt.Equal(requestedAt) ||
		assigned.AssignedAt == nil {
		t.Errorf("recovered cancellation = %#v", pending)
	}

	finishedAt := requestedAt.Add(2 * time.Minute)
	observation := run.Observation{
		ID: record.ID, ClusterName: "colo", Phase: run.PhaseFinished,
		Outcome: &canceled, Reason: new(run.ReasonCancelRequested), ObservedAt: finishedAt,
	}
	if err := reopened.ApplyRunObservation(ctx, observation); err != nil {
		t.Fatalf("complete canceled Run: %v", err)
	}
	if err := reopened.ApplyRunObservation(ctx, observation); err != nil {
		t.Fatalf("repeat canceled observation: %v", err)
	}
	completed, err := reopened.GetRun(ctx, record.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	state, err := completed.CancellationState()
	if err != nil {
		t.Fatalf("CancellationState() error = %v", err)
	}
	if state != run.CancellationStateCompleted || completed.FinishedAt == nil ||
		!completed.FinishedAt.Equal(finishedAt) {
		t.Errorf("completed cancellation = %#v, state %q", completed, state)
	}
	if _, err := reopened.GetRunToCancelForCluster(ctx, "colo"); !errors.Is(err, run.ErrNoRunToCancel) {
		t.Errorf("completed cancellation command error = %v, want no command", err)
	}
}

func singleCancellationTestRun(t *testing.T, ctx context.Context, database *Database) run.Run {
	t.Helper()

	first, second := createCapacityTestRuns(t, ctx, database)
	if _, err := database.pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, second.ID); err != nil {
		t.Fatalf("delete unused Run: %v", err)
	}

	return first
}
