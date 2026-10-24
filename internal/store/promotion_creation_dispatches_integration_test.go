package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestPromotionCreationDispatchLifecycleIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	record := createDispatchablePromotion(t, fixture, createdAt.Add(2*time.Minute))
	delays := []time.Duration{5 * time.Second, 30 * time.Second}

	claimed, err := fixture.database.ClaimPromotionCreation(fixture.ctx, promotion.CreationDispatchClaim{
		ClusterName:      "colo",
		ClaimedAt:        record.CreatedAt,
		RetryDelays:      delays,
		ExhaustedMessage: "dispatch attempts exhausted",
	})
	if err != nil {
		t.Fatalf("first ClaimPromotionCreation() error = %v", err)
	}
	if claimed.ID != record.ID || claimed.DispatchAttemptCount != 1 ||
		claimed.LastDispatchAt == nil || !claimed.LastDispatchAt.Equal(record.CreatedAt) ||
		claimed.NextDispatchAt == nil || !claimed.NextDispatchAt.Equal(record.CreatedAt.Add(delays[0])) {
		t.Errorf("first dispatch claim = %#v", claimed)
	}
	if _, err := fixture.database.ClaimPromotionCreation(fixture.ctx, promotion.CreationDispatchClaim{
		ClusterName:      "colo",
		ClaimedAt:        record.CreatedAt.Add(time.Second),
		RetryDelays:      delays,
		ExhaustedMessage: "dispatch attempts exhausted",
	}); !errors.Is(err, promotion.ErrNoCreationDispatch) {
		t.Fatalf("early claim error = %v, want no dispatch", err)
	}

	diagnostic := "apiserver unavailable"
	retrying, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "colo",
		Attempt:     1,
		Result:      promotion.CreationDispatchRetryable,
		Diagnostic:  &diagnostic,
		ReportedAt:  record.CreatedAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("retryable ReportPromotionCreation() error = %v", err)
	}
	if retrying.Message == nil || *retrying.Message != diagnostic ||
		retrying.NextDispatchAt == nil || !retrying.NextDispatchAt.Equal(record.CreatedAt.Add(delays[0])) {
		t.Errorf("retryable dispatch = %#v", retrying)
	}

	claimed, err = fixture.database.ClaimPromotionCreation(fixture.ctx, promotion.CreationDispatchClaim{
		ClusterName:      "colo",
		ClaimedAt:        record.CreatedAt.Add(delays[0]),
		RetryDelays:      delays,
		ExhaustedMessage: "dispatch attempts exhausted",
	})
	if err != nil {
		t.Fatalf("second ClaimPromotionCreation() error = %v", err)
	}
	if claimed.DispatchAttemptCount != 2 || claimed.NextDispatchAt == nil ||
		!claimed.NextDispatchAt.Equal(record.CreatedAt.Add(delays[0]+delays[1])) {
		t.Errorf("second dispatch claim = %#v", claimed)
	}
	staleDiagnostic := "late failure from attempt one"
	stale, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "colo",
		Attempt:     1,
		Result:      promotion.CreationDispatchFailed,
		Diagnostic:  &staleDiagnostic,
		ReportedAt:  record.CreatedAt.Add(delays[0] + time.Second),
	})
	if err != nil {
		t.Fatalf("stale ReportPromotionCreation() error = %v", err)
	}
	if stale.Status != promotion.StatusCreating || stale.DispatchAttemptCount != 2 {
		t.Errorf("stale dispatch report changed Promotion = %#v", stale)
	}

	uid := "promotion-uid"
	dispatchedAt := record.CreatedAt.Add(delays[0] + 2*time.Second)
	dispatched, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "colo",
		Attempt:     2,
		Result:      promotion.CreationDispatchCreated,
		KargoUID:    &uid,
		ReportedAt:  dispatchedAt,
	})
	if err != nil {
		t.Fatalf("created ReportPromotionCreation() error = %v", err)
	}
	if dispatched.Status != promotion.StatusCreating || dispatched.NextDispatchAt != nil ||
		dispatched.KargoPromotionUID == nil || *dispatched.KargoPromotionUID != uid ||
		dispatched.DispatchedAt == nil || !dispatched.DispatchedAt.Equal(dispatchedAt) {
		t.Errorf("created dispatch = %#v", dispatched)
	}
	if _, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "colo",
		Attempt:     2,
		Result:      promotion.CreationDispatchCreated,
		KargoUID:    &uid,
		ReportedAt:  dispatchedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("repeated created report error = %v", err)
	}
	conflictingUID := "another-promotion-uid"
	if _, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "colo",
		Attempt:     2,
		Result:      promotion.CreationDispatchCreated,
		KargoUID:    &conflictingUID,
		ReportedAt:  dispatchedAt.Add(2 * time.Second),
	}); !errors.Is(err, promotion.ErrCreationDispatchConflict) {
		t.Fatalf("conflicting UID error = %v", err)
	}
}

func TestPromotionCreationDispatchExhaustionDoesNotBlockLaterWorkIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 19, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	first := createDispatchablePromotion(t, fixture, createdAt.Add(2*time.Minute))
	second := createDispatchablePromotion(t, fixture, createdAt.Add(3*time.Minute))
	claim := promotion.CreationDispatchClaim{
		ClusterName:      "colo",
		ClaimedAt:        first.CreatedAt,
		RetryDelays:      []time.Duration{time.Second},
		ExhaustedMessage: "dispatch attempts exhausted",
	}

	claimed, err := fixture.database.ClaimPromotionCreation(fixture.ctx, claim)
	if err != nil || claimed.ID != first.ID {
		t.Fatalf("first claim = %#v, %v", claimed, err)
	}
	claim.ClaimedAt = second.CreatedAt
	claimed, err = fixture.database.ClaimPromotionCreation(fixture.ctx, claim)
	if err != nil || claimed.ID != second.ID {
		t.Fatalf("claim after exhaustion = %#v, %v", claimed, err)
	}
	exhausted, err := fixture.database.GetPromotion(fixture.ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exhausted.Status != promotion.StatusError || exhausted.NextDispatchAt != nil ||
		exhausted.Message == nil || *exhausted.Message != claim.ExhaustedMessage {
		t.Errorf("exhausted Promotion = %#v", exhausted)
	}
	claim.ClaimedAt = second.CreatedAt.Add(time.Second)
	if _, err := fixture.database.ClaimPromotionCreation(fixture.ctx, claim); !errors.Is(
		err,
		promotion.ErrNoCreationDispatch,
	) {
		t.Fatalf("single exhausted claim error = %v", err)
	}
	exhausted, err = fixture.database.GetPromotion(fixture.ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exhausted.Status != promotion.StatusError {
		t.Errorf("single exhausted Promotion = %#v", exhausted)
	}
}

func TestPromotionCreationDispatchRejectsWrongClusterAndTerminallyFailsIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 20, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	record := createDispatchablePromotion(t, fixture, createdAt.Add(2*time.Minute))
	if _, err := fixture.database.ClaimPromotionCreation(fixture.ctx, promotion.CreationDispatchClaim{
		ClusterName:      "edge",
		ClaimedAt:        record.CreatedAt,
		RetryDelays:      []time.Duration{time.Second},
		ExhaustedMessage: "dispatch attempts exhausted",
	}); !errors.Is(err, promotion.ErrNoCreationDispatch) {
		t.Fatalf("wrong-cluster claim error = %v", err)
	}
	if _, err := fixture.database.ClaimPromotionCreation(fixture.ctx, promotion.CreationDispatchClaim{
		ClusterName:      "colo",
		ClaimedAt:        record.CreatedAt,
		RetryDelays:      []time.Duration{time.Minute},
		ExhaustedMessage: "dispatch attempts exhausted",
	}); err != nil {
		t.Fatal(err)
	}
	diagnostic := "Kargo admission denied the Promotion"
	failed, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "colo",
		Attempt:     1,
		Result:      promotion.CreationDispatchFailed,
		Diagnostic:  &diagnostic,
		ReportedAt:  record.CreatedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != promotion.StatusError || failed.NextDispatchAt != nil ||
		failed.Message == nil || *failed.Message != diagnostic {
		t.Errorf("failed Promotion = %#v", failed)
	}
	if _, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "edge",
		Attempt:     1,
		Result:      promotion.CreationDispatchFailed,
		Diagnostic:  &diagnostic,
		ReportedAt:  record.CreatedAt.Add(2 * time.Second),
	}); !errors.Is(err, promotion.ErrPromotionNotFound) {
		t.Fatalf("wrong-cluster report error = %v", err)
	}
}

func TestConcurrentPromotionCreationDispatchClaimsReturnOneCommandIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 21, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	record := createDispatchablePromotion(t, fixture, createdAt.Add(2*time.Minute))
	claim := promotion.CreationDispatchClaim{
		ClusterName:      "colo",
		ClaimedAt:        record.CreatedAt,
		RetryDelays:      []time.Duration{time.Minute},
		ExhaustedMessage: "dispatch attempts exhausted",
	}

	type result struct {
		record promotion.Promotion
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			claimed, err := fixture.database.ClaimPromotionCreation(fixture.ctx, claim)
			results <- result{record: claimed, err: err}
		}()
	}
	claimedCount := 0
	noWorkCount := 0
	for range 2 {
		result := <-results
		switch {
		case result.err == nil && result.record.ID == record.ID:
			claimedCount++
		case errors.Is(result.err, promotion.ErrNoCreationDispatch):
			noWorkCount++
		default:
			t.Fatalf("concurrent dispatch claim = %#v, %v", result.record, result.err)
		}
	}
	if claimedCount != 1 || noWorkCount != 1 {
		t.Errorf("concurrent results: claimed = %d, no work = %d", claimedCount, noWorkCount)
	}
}

func createDispatchablePromotion(
	t *testing.T,
	fixture promotionIntegrationFixture,
	acceptedAt time.Time,
) promotion.Promotion {
	t.Helper()

	stage := fixture.stage
	stage.ResourceVersion = uuid.NewString()
	stage.Freight = json.RawMessage(`{"items":[{
      "origin":"Warehouse/external-dns",
      "name":"freight-42",
      "artifacts":[{"kind":"image","version":"1.2.3"}]
    }]}`)
	stage.FirstObservedAt = acceptedAt.Add(-30 * time.Second)
	stage.LastObservedAt = acceptedAt.Add(-30 * time.Second)
	if err := fixture.database.ReplacePromotionStageSnapshot(
		fixture.ctx,
		promotion.StageSnapshot{
			SnapshotID:  uuid.New(),
			Integration: stage.Integration,
			ClusterName: stage.ClusterName,
			ObservedAt:  stage.LastObservedAt,
			AcceptedAt:  stage.LastObservedAt.Add(time.Second),
		},
		[]promotion.Stage{stage},
	); err != nil {
		t.Fatalf("replace dispatch Stage snapshot: %v", err)
	}

	return mustCreateOperatorPromotion(t, fixture, stage.ID, acceptedAt)
}

func mustCreateOperatorPromotion(
	t *testing.T,
	fixture promotionIntegrationFixture,
	stageID uuid.UUID,
	acceptedAt time.Time,
) promotion.Promotion {
	t.Helper()

	record, err := fixture.database.CreateOperatorPromotion(fixture.ctx, promotion.OperatorPromotionAcceptance{
		ID:                         uuid.New(),
		Integration:                fixture.stage.Integration,
		TargetStageID:              stageID,
		SourceStageID:              stageID,
		FreightName:                "freight-42",
		RequestedByPrincipalID:     fixture.principal.ID,
		RequestedBySnapshot:        fixture.actor,
		AcceptedAt:                 acceptedAt,
		MaximumStageObservationAge: 5 * time.Minute,
		PendingReviewRetention:     8 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateOperatorPromotion() error = %v", err)
	}

	return record
}
