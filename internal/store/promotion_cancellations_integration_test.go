package store

import (
	"errors"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/cancellation"
	"github.com/slipramp/slipramp/internal/promotion"
)

func TestCancelUndispatchedPromotionIntegration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	fixture := newPromotionIntegrationFixture(t, now)
	record := createDispatchablePromotion(t, fixture, now.Add(2*time.Minute))
	at := record.CreatedAt.Add(time.Second)
	for _, attempt := range []time.Time{at, at.Add(time.Minute)} {
		got, _, err := fixture.database.CancelPromotion(fixture.ctx, record.ID, attempt, fixture.actor)
		if err != nil {
			t.Fatal(err)
		}
		if got.CancellationState() != cancellation.Completed || !got.Cancellation.RequestedAt.Equal(at) ||
			got.Cancellation.RequestedBy.PrincipalID != fixture.actor.PrincipalID || got.KargoExecution.Outcome != nil ||
			got.Lifecycle.State != promotion.LifecycleApproved {
			t.Fatalf("unexpected canceled Promotion: %#v", got)
		}
	}
	_, err := fixture.database.claimPromotionCreationAt(fixture.ctx, promotion.CreationDispatchClaim{
		ClusterName:            fixture.stage.ClusterName,
		GateEvaluatorProcessID: fixture.principal.ID,
		ClaimedAt:              at.Add(time.Hour),
		RetryDelays:            []time.Duration{time.Minute},
		ExhaustedMessage:       "delivery failed",
	})
	if !errors.Is(err, promotion.ErrNoCreationDispatch) {
		t.Fatalf("canceled creation claim = %v", err)
	}
}

func TestPromotionCancellationDuringPreparationIntegration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	fixture := newPromotionIntegrationFixture(t, now)
	preparation := preparePromotionForCompletion(t, fixture, "cancel-preparing", now.Add(2*time.Minute))
	at := preparation.completedAt
	record, _, err := fixture.database.CancelPromotion(fixture.ctx, preparation.record.ID, at, fixture.actor)
	if err != nil || record.CancellationState() != cancellation.Requested {
		t.Fatalf("request cancellation during preparation: %v, %v", record.CancellationState(), err)
	}
	linked, err := fixture.database.GetRun(fixture.ctx, preparation.runID)
	if err != nil || linked.CanceledBy == nil || linked.CanceledBy.PrincipalID != fixture.actor.PrincipalID ||
		linked.CancellationRequestedAt == nil || !linked.CancellationRequestedAt.Equal(at) {
		t.Fatalf("linked Run cancellation: %#v, %v", linked.CancellationRequestedAt, err)
	}
	_, err = fixture.database.GetPromotionWithFinishedPreparation(fixture.ctx, at)
	if !errors.Is(err, promotion.ErrNoPreparationWork) {
		t.Fatalf("canceled preparation remained eligible: %v", err)
	}
	claimed, err := fixture.database.ClaimPromotionCancellation(fixture.ctx, fixture.stage.ClusterName, at, at.Add(time.Minute))
	if err != nil || claimed.ID != record.ID {
		t.Fatalf("claim cancellation: %v", err)
	}
	outcome := promotion.KargoPhaseSucceeded
	record, err = fixture.database.ReportPromotionReconciliation(fixture.ctx, promotion.ReconciliationReport{
		PromotionID: record.ID, ClusterName: fixture.stage.ClusterName, KargoName: record.ExternalPromotionID,
		KargoUID: "observed-uid", Phase: &outcome, ObservedAt: at.Add(time.Second),
	})
	if err != nil || record.CancellationState() != cancellation.Overtaken || record.Lifecycle.State != promotion.LifecyclePreparing {
		t.Fatalf("record competing Kargo outcome: %v, %v", record.CancellationState(), err)
	}
}

func TestCancelPromotionWithUnreportedCreationIntegration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	fixture := newPromotionIntegrationFixture(t, now)
	ctx, database := fixture.ctx, fixture.database
	record := createDispatchablePromotion(t, fixture, now.Add(2*time.Minute))
	at := record.CreatedAt.Add(time.Second)
	_, err := database.claimPromotionCreationAt(ctx, promotion.CreationDispatchClaim{
		ClusterName:            fixture.stage.ClusterName,
		GateEvaluatorProcessID: fixture.principal.ID,
		ClaimedAt:              at,
		RetryDelays:            []time.Duration{time.Minute},
		ExhaustedMessage:       "delivery failed",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Admission assigned a different object name. A callback can race both the
	// creation receipt and the operator's cancellation request.
	name := "admitted-name"
	callback := fixture.acceptance(t, fixture.request(name), at.Add(time.Second))
	if _, err := database.AcceptPromotionRequest(ctx, callback); !errors.Is(err, promotion.ErrCreationIdentityPending) {
		t.Fatalf("callback before receipt: %v", err)
	}
	if _, _, err := database.CancelPromotion(ctx, record.ID, at.Add(2*time.Second), fixture.actor); err != nil {
		t.Fatal(err)
	}
	report := promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: fixture.stage.ClusterName,
		Attempt:     1,
		Result:      promotion.CreationDispatchAbsent,
		ReportedAt:  at.Add(3 * time.Second),
	}
	absent, err := database.ReportPromotionCreation(ctx, report)
	if err != nil || absent.CancellationState() != cancellation.Completed {
		t.Fatalf("confirm missing object: %v, %v", absent.CancellationState(), err)
	}
	report.Result = promotion.CreationDispatchCreated
	report.KargoName = &name
	report.KargoUID = new("recovered-uid")
	report.ReportedAt = at.Add(4 * time.Second)
	recovered, err := database.ReportPromotionCreation(ctx, report)
	if err != nil || recovered.CancellationState() != cancellation.Requested || recovered.ExternalPromotionID != name {
		t.Fatalf("late receipt: %v, %v", recovered.CancellationState(), err)
	}
	callback.AcceptedAt = at.Add(5 * time.Second)
	repeated, err := database.AcceptPromotionRequest(ctx, callback)
	if err != nil || repeated.ID != record.ID || repeated.CancellationState() != cancellation.Requested {
		t.Fatalf("callback matched Promotion %s: %v", repeated.ID, err)
	}
}
