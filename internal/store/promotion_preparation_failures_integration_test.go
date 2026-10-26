package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

func TestPromotionPreparationDispatchFailureIsolationAndRecoveryIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	acceptedAt := createdAt.Add(2 * time.Minute)
	if _, err := fixture.database.UpsertPromotionStagePolicy(fixture.ctx, promotion.StagePolicy{
		StageID:              fixture.stage.ID,
		Mode:                 promotion.PolicyModeReviewRequired,
		UpdatedByPrincipalID: fixture.principal.ID,
		UpdatedBySnapshot:    fixture.actor,
		UpdatedAt:            acceptedAt.Add(-time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 0, 2)
	for index, name := range []string{"first", "second"} {
		record, err := fixture.database.AcceptPromotionRequest(fixture.ctx, fixture.acceptance(
			t, fixture.request(name), acceptedAt.Add(time.Duration(index)*time.Second),
		))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, record.ID)
	}

	failure := promotion.PreparationFailure{
		PromotionID: ids[0],
		FailedAt:    acceptedAt.Add(2 * time.Second),
		Diagnostic:  "Promotion preparation could not complete; check Server configuration and dependency health.",
	}
	state, recorded, err := fixture.database.RecordPromotionPreparationFailure(fixture.ctx, failure)
	if err != nil || !recorded || state.Count != 1 || state.Terminal {
		t.Fatalf("first failure = %#v, %t, %v", state, recorded, err)
	}
	if _, recorded, err := fixture.database.RecordPromotionPreparationFailure(fixture.ctx, failure); err != nil || recorded {
		t.Fatalf("failure replay before due = %t, %v", recorded, err)
	}
	if next, err := fixture.database.GetPromotionAwaitingDispatch(fixture.ctx, failure.FailedAt); err != nil || next.ID != ids[1] {
		t.Fatalf("later work during backoff = %s, %v", next.ID, err)
	}

	fixture.database.Close()
	database, err := Open(fixture.ctx, fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for count := 1; count <= promotion.PreparationFailureLimit; count++ {
		record, err := database.GetPromotion(fixture.ctx, ids[0])
		if err != nil || record.PreparationFailureCount != count {
			t.Fatalf("persisted failure %d = %#v, %v", count, record, err)
		}
		if count == promotion.PreparationFailureLimit {
			if record.Status != promotion.StatusError || record.NextPreparationAttemptAt != nil ||
				record.Message == nil || !strings.Contains(*record.Message, "retry budget exhausted") {
				t.Fatalf("exhausted preparation = %#v", record)
			}
			break
		}
		delay := min(promotion.PreparationInitialRetryDelay<<uint(count-1), promotion.PreparationMaximumRetryDelay)
		if record.NextPreparationAttemptAt == nil || !record.NextPreparationAttemptAt.Equal(failure.FailedAt.Add(delay)) {
			t.Fatalf("failure %d due time = %v, want %v", count, record.NextPreparationAttemptAt, failure.FailedAt.Add(delay))
		}
		failure.FailedAt = *record.NextPreparationAttemptAt
		state, recorded, err = database.RecordPromotionPreparationFailure(fixture.ctx, failure)
		if err != nil || !recorded || state.Count != count+1 {
			t.Fatalf("failure %d = %#v, %t, %v", count+1, state, recorded, err)
		}
	}

	if next, err := database.GetPromotionAwaitingDispatch(fixture.ctx, failure.FailedAt.Add(time.Hour)); err != nil || next.ID != ids[1] {
		t.Fatalf("later work after exhaustion = %s, %v", next.ID, err)
	}
	preparationRun, err := database.GetRun(fixture.ctx, fixture.runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ReservePromotionPreparationRevision(
		fixture.ctx, ids[1], preparationRun.RepositoryID, preparationRun.CatalogRevisionID, failure.FailedAt,
	); err != nil {
		t.Fatal(err)
	}
	failure.PromotionID = ids[1]
	failure.Permanent = true
	state, recorded, err = database.RecordPromotionPreparationFailure(fixture.ctx, failure)
	if err != nil || !recorded || !state.Terminal || state.Count != 1 {
		t.Fatalf("permanent failure = %#v, %t, %v", state, recorded, err)
	}
	if _, err := database.GetPromotionAwaitingDispatch(fixture.ctx, failure.FailedAt.Add(time.Hour)); !errors.Is(err, promotion.ErrNoPreparationWork) {
		t.Fatalf("terminal preparation remains eligible: %v", err)
	}
	preparationRun.ID = uuid.New()
	preparationRun.Source = run.Source{
		Type:      run.SourceTypePromotion,
		Promotion: &run.PromotionRunSource{PromotionID: ids[1]},
	}
	preparationRun.IdempotencyKey = new(ids[1].String())
	preparationRun.RequestDigest = strings.Repeat("d", 64)
	preparationRun.CreatedBy = &fixture.actor
	preparationRun.CreatedAt = failure.FailedAt.Add(time.Second)
	if _, err := database.CreateRun(fixture.ctx, preparationRun, nil); !errors.Is(err, run.ErrInvalidRequest) {
		t.Fatalf("creation after terminal preparation = %v", err)
	}
	assertRunCounts(t, database, 1, 1)
}

func TestPromotionFinishedPreparationFailureIsolationIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	first := preparePromotionForCompletion(t, fixture, "first", createdAt.Add(2*time.Minute))
	second := preparePromotionForCompletion(t, fixture, "second", createdAt.Add(3*time.Minute))
	attemptedAt := createdAt.Add(4 * time.Minute)
	for _, prepared := range []promotionPreparationCompletion{first, second} {
		if _, err := fixture.database.CancelRun(fixture.ctx, prepared.runID, attemptedAt, nil); err != nil {
			t.Fatal(err)
		}
	}
	if next, err := fixture.database.GetPromotionWithFinishedPreparation(fixture.ctx, attemptedAt); err != nil || next.ID != first.record.ID {
		t.Fatalf("oldest finished preparation = %s, %v", next.ID, err)
	}
	failure := promotion.PreparationFailure{
		PromotionID:      first.record.ID,
		PreparationRunID: &first.runID,
		FailedAt:         attemptedAt,
		Diagnostic:       "Preparation artifacts could not be read.",
	}
	if state, recorded, err := fixture.database.RecordPromotionPreparationFailure(fixture.ctx, failure); err != nil || !recorded || state.Terminal {
		t.Fatalf("reconciliation failure = %#v, %t, %v", state, recorded, err)
	}
	if next, err := fixture.database.GetPromotionWithFinishedPreparation(fixture.ctx, attemptedAt); err != nil || next.ID != second.record.ID {
		t.Fatalf("later reconciliation during backoff = %s, %v", next.ID, err)
	}
	attemptedAt = attemptedAt.Add(promotion.PreparationInitialRetryDelay)
	if next, err := fixture.database.GetPromotionWithFinishedPreparation(fixture.ctx, attemptedAt); err != nil || next.ID != first.record.ID {
		t.Fatalf("due reconciliation = %s, %v", next.ID, err)
	}
	failure.PreparationRunID = nil
	failure.FailedAt = attemptedAt
	if _, recorded, err := fixture.database.RecordPromotionPreparationFailure(fixture.ctx, failure); err != nil || recorded {
		t.Fatalf("late dispatch failure after Run attachment = %t, %v", recorded, err)
	}
	failed, err := fixture.database.FailPromotionPreparation(
		fixture.ctx, first.record.ID, first.runID, "Preparation Run finished with outcome canceled.", attemptedAt,
	)
	if err != nil || failed.Status != promotion.StatusError || failed.NextPreparationAttemptAt != nil {
		t.Fatalf("finished canceled preparation = %#v, %v", failed, err)
	}
}
