package scheduletrigger

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/run"
)

func TestSchedulerCreatesDueRunWithUTCSource(t *testing.T) {
	start := time.Date(2026, time.November, 3, 2, 59, 0, 0, time.UTC)
	clock := clocktesting.NewFakeClock(start)
	catalogs := &schedulerCatalog{target: scheduledTarget("0 3 * * *")}
	runs := &recordingRunCreator{}
	scheduler := testScheduler(catalogs, runs, clock)

	if err := scheduler.Evaluate(t.Context()); err != nil {
		t.Fatalf("initial Evaluate() error = %v", err)
	}
	if len(runs.requests) != 0 {
		t.Fatalf("initial Run requests = %d, want 0", len(runs.requests))
	}

	clock.Step(time.Minute)
	if err := scheduler.Evaluate(t.Context()); err != nil {
		t.Fatalf("due Evaluate() error = %v", err)
	}
	if len(runs.requests) != 1 {
		t.Fatalf("due Run requests = %d, want 1", len(runs.requests))
	}
	request := runs.requests[0]
	wantScheduledAt := time.Date(2026, time.November, 3, 3, 0, 0, 0, time.UTC)
	if request.Source.Type != run.SourceTypeSchedule || request.Source.Schedule == nil ||
		request.Source.Schedule.TriggerName != "nightly-preview" ||
		!request.Source.Schedule.ScheduledAt.Equal(wantScheduledAt) || request.Actor != nil ||
		request.CatalogRevisionID != nil || request.Parameters["refresh"] != true ||
		request.IdempotencyKey == nil || *request.IdempotencyKey != run.ScheduleOccurrenceIdempotencyKey(
		testTargetID,
		"preview",
		"nightly-preview",
		wantScheduledAt,
	) {
		t.Errorf("scheduled Run request = %#v", request)
	}

	if err := scheduler.Evaluate(t.Context()); err != nil {
		t.Fatalf("duplicate Evaluate() error = %v", err)
	}
	if len(runs.requests) != 1 {
		t.Errorf("Run requests after duplicate evaluation = %d, want 1", len(runs.requests))
	}
}

func TestSchedulerSkipsOverlapWithoutRetryingOccurrence(t *testing.T) {
	start := time.Date(2026, time.November, 3, 2, 59, 0, 0, time.UTC)
	clock := clocktesting.NewFakeClock(start)
	catalogs := &schedulerCatalog{target: scheduledTarget("0 3 * * *")}
	runs := &recordingRunCreator{err: run.ErrScheduleOverlap}
	scheduler := testScheduler(catalogs, runs, clock)

	mustEvaluate(t, scheduler)
	clock.Step(time.Minute)
	mustEvaluate(t, scheduler)
	mustEvaluate(t, scheduler)
	if len(runs.requests) != 1 {
		t.Fatalf("overlap Run requests = %d, want 1", len(runs.requests))
	}

	clock.Step(24 * time.Hour)
	mustEvaluate(t, scheduler)
	if len(runs.requests) != 2 {
		t.Errorf("next occurrence Run requests = %d, want 2", len(runs.requests))
	}
}

func TestSchedulerDoesNotRetryCreationFailure(t *testing.T) {
	start := time.Date(2026, time.November, 3, 2, 59, 0, 0, time.UTC)
	clock := clocktesting.NewFakeClock(start)
	runs := &recordingRunCreator{err: run.ErrCatalogNotFound}
	scheduler := testScheduler(
		&schedulerCatalog{target: scheduledTarget("0 3 * * *")},
		runs,
		clock,
	)

	mustEvaluate(t, scheduler)
	clock.Step(time.Minute)
	mustEvaluate(t, scheduler)
	mustEvaluate(t, scheduler)
	if len(runs.requests) != 1 {
		t.Errorf("failed occurrence Run requests = %d, want 1", len(runs.requests))
	}
}

func TestSchedulerDoesNotCatchUpAfterStartup(t *testing.T) {
	start := time.Date(2026, time.November, 3, 3, 1, 0, 0, time.UTC)
	clock := clocktesting.NewFakeClock(start)
	runs := &recordingRunCreator{}
	scheduler := testScheduler(
		&schedulerCatalog{target: scheduledTarget("0 3 * * *")},
		runs,
		clock,
	)

	mustEvaluate(t, scheduler)
	if len(runs.requests) != 0 {
		t.Fatalf("startup Run requests = %d, want 0", len(runs.requests))
	}

	clock.Step(23*time.Hour + 59*time.Minute)
	mustEvaluate(t, scheduler)
	if len(runs.requests) != 1 ||
		!runs.requests[0].Source.Schedule.ScheduledAt.Equal(start.Add(23*time.Hour+59*time.Minute)) {
		t.Errorf("next future occurrence requests = %#v", runs.requests)
	}
}

func TestSchedulerRecalculatesAfterScheduleChange(t *testing.T) {
	start := time.Date(2026, time.November, 3, 2, 0, 0, 0, time.UTC)
	clock := clocktesting.NewFakeClock(start)
	catalogs := &schedulerCatalog{target: scheduledTarget("0 3 * * *")}
	runs := &recordingRunCreator{}
	scheduler := testScheduler(catalogs, runs, clock)

	mustEvaluate(t, scheduler)
	clock.Step(15 * time.Minute)
	catalogs.target.CatalogRevisionID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1402")
	action := catalogs.target.Target.Spec.Actions["preview"]
	action.Triggers[0].Schedule.Cron = "30 2 * * *"
	catalogs.target.Target.Spec.Actions["preview"] = action
	mustEvaluate(t, scheduler)

	clock.Step(15 * time.Minute)
	mustEvaluate(t, scheduler)
	if len(runs.requests) != 1 ||
		!runs.requests[0].Source.Schedule.ScheduledAt.Equal(time.Date(2026, time.November, 3, 2, 30, 0, 0, time.UTC)) {
		t.Errorf("changed schedule requests = %#v", runs.requests)
	}
}

func TestSchedulerDropsRemovedSchedule(t *testing.T) {
	start := time.Date(2026, time.November, 3, 2, 59, 0, 0, time.UTC)
	clock := clocktesting.NewFakeClock(start)
	catalogs := &schedulerCatalog{target: scheduledTarget("0 3 * * *")}
	runs := &recordingRunCreator{}
	scheduler := testScheduler(catalogs, runs, clock)

	mustEvaluate(t, scheduler)
	action := catalogs.target.Target.Spec.Actions["preview"]
	action.Triggers = nil
	catalogs.target.Target.Spec.Actions["preview"] = action
	catalogs.target.CatalogRevisionID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1403")
	clock.Step(time.Minute)
	mustEvaluate(t, scheduler)
	if len(runs.requests) != 0 {
		t.Errorf("removed schedule Run requests = %d, want 0", len(runs.requests))
	}
}

var testTargetID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1400")

func scheduledTarget(expression string) catalog.TargetSummary {
	return catalog.TargetSummary{
		ID:                testTargetID,
		CatalogRevisionID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1401"),
		Target: catalog.Target{
			Spec: catalog.TargetSpec{Actions: map[string]catalog.Action{
				"preview": {
					Command: []string{"preview"},
					Triggers: []catalog.Trigger{{
						Name:       "nightly-preview",
						Schedule:   &catalog.ScheduleTrigger{Cron: expression},
						Parameters: map[string]any{"refresh": true},
					}},
				},
			}},
		},
	}
}

type schedulerCatalog struct {
	target catalog.TargetSummary
}

func (s *schedulerCatalog) ListTargets(
	_ context.Context,
	filter catalog.TargetListFilter,
) (catalog.TargetList, error) {
	if filter.Offset > 0 {
		return catalog.TargetList{Items: []catalog.TargetSummary{}, Total: 1}, nil
	}

	return catalog.TargetList{Items: []catalog.TargetSummary{s.target}, Total: 1}, nil
}

type recordingRunCreator struct {
	requests []run.CreateRequest
	err      error
}

func (r *recordingRunCreator) CreateRun(_ context.Context, request run.CreateRequest) (run.Run, error) {
	r.requests = append(r.requests, request)

	return run.Run{ID: uuid.New()}, r.err
}

func testScheduler(
	catalogs Catalog,
	runs RunCreator,
	clock *clocktesting.FakeClock,
) *Scheduler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewScheduler(catalogs, runs, time.Second, logger, WithClock(clock))
}

func mustEvaluate(t *testing.T, scheduler *Scheduler) {
	t.Helper()
	if err := scheduler.Evaluate(t.Context()); err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
}
