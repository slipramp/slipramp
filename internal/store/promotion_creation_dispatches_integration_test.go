package store

import (
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

	name := "external-dns-prod.01test.freight"
	uid := "promotion-uid"
	dispatchedAt := record.CreatedAt.Add(delays[0] + 2*time.Second)
	dispatched, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "colo",
		Attempt:     2,
		Result:      promotion.CreationDispatchCreated,
		KargoName:   &name,
		KargoUID:    &uid,
		ReportedAt:  dispatchedAt,
	})
	if err != nil {
		t.Fatalf("created ReportPromotionCreation() error = %v", err)
	}
	if dispatched.Status != promotion.StatusCreating || dispatched.NextDispatchAt != nil ||
		dispatched.ExternalPromotionID != name ||
		dispatched.KargoPromotionUID == nil || *dispatched.KargoPromotionUID != uid ||
		dispatched.DispatchedAt == nil || !dispatched.DispatchedAt.Equal(dispatchedAt) {
		t.Errorf("created dispatch = %#v", dispatched)
	}
	if _, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: "colo",
		Attempt:     2,
		Result:      promotion.CreationDispatchCreated,
		KargoName:   &name,
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
		KargoName:   &name,
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

func TestPromotionCreationReconciliationLifecycleIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 22, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	record := createDispatchablePromotion(t, fixture, createdAt.Add(2*time.Minute))
	materializedAt := record.CreatedAt.Add(time.Second)
	materialized := materializeOperatorPromotion(t, fixture, record, materializedAt)
	if materialized.NextReconciliationAt == nil || !materialized.NextReconciliationAt.Equal(materializedAt) {
		t.Fatalf("materialized Promotion reconciliation time = %v", materialized.NextReconciliationAt)
	}

	interval := 15 * time.Second
	claimed, err := fixture.database.ClaimPromotionCreationReconciliation(
		fixture.ctx,
		promotion.CreationReconciliationClaim{
			ClusterName: "colo",
			ClaimedAt:   materializedAt,
			Interval:    interval,
		},
	)
	if err != nil {
		t.Fatalf("ClaimPromotionCreationReconciliation() error = %v", err)
	}
	if claimed.ID != record.ID || claimed.LastReconciliationAt == nil ||
		!claimed.LastReconciliationAt.Equal(materializedAt) || claimed.NextReconciliationAt == nil ||
		!claimed.NextReconciliationAt.Equal(materializedAt.Add(interval)) {
		t.Errorf("reconciliation claim = %#v", claimed)
	}
	if _, err := fixture.database.ClaimPromotionCreationReconciliation(
		fixture.ctx,
		promotion.CreationReconciliationClaim{
			ClusterName: "colo",
			ClaimedAt:   materializedAt.Add(time.Second),
			Interval:    interval,
		},
	); !errors.Is(err, promotion.ErrNoCreationReconciliation) {
		t.Fatalf("early reconciliation claim error = %v", err)
	}

	diagnostic := "Kargo Promotion kargo-core/external-dns-prod.01test.freight reached terminal phase Failed before Slipramp received its callback."
	failed, err := fixture.database.ReportPromotionCreationReconciliation(
		fixture.ctx,
		promotion.CreationReconciliationReport{
			PromotionID: record.ID,
			ClusterName: "colo",
			KargoName:   materialized.ExternalPromotionID,
			KargoUID:    *materialized.KargoPromotionUID,
			Phase:       promotion.CreationReconciliationFailed,
			Diagnostic:  diagnostic,
			ReportedAt:  materializedAt.Add(2 * time.Second),
		},
	)
	if err != nil {
		t.Fatalf("ReportPromotionCreationReconciliation() error = %v", err)
	}
	if failed.Status != promotion.StatusError || failed.NextReconciliationAt != nil ||
		failed.Message == nil || *failed.Message != diagnostic {
		t.Errorf("terminal reconciliation = %#v", failed)
	}

	repeated, err := fixture.database.ReportPromotionCreationReconciliation(
		fixture.ctx,
		promotion.CreationReconciliationReport{
			PromotionID: record.ID,
			ClusterName: "colo",
			KargoName:   materialized.ExternalPromotionID,
			KargoUID:    *materialized.KargoPromotionUID,
			Phase:       promotion.CreationReconciliationFailed,
			Diagnostic:  diagnostic,
			ReportedAt:  materializedAt.Add(3 * time.Second),
		},
	)
	if err != nil || repeated.Status != promotion.StatusError || repeated.Message == nil ||
		*repeated.Message != diagnostic {
		t.Errorf("repeated reconciliation = %#v, %v", repeated, err)
	}
}

func TestPromotionCreationReconciliationIsCallbackRaceSafeIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 23, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)

	callbackWinner := createDispatchablePromotion(t, fixture, createdAt.Add(2*time.Minute))
	callbackWinner = materializeOperatorPromotion(t, fixture, callbackWinner, callbackWinner.CreatedAt.Add(time.Second))
	callback := fixture.acceptance(
		t,
		fixture.request(callbackWinner.ExternalPromotionID),
		callbackWinner.CreatedAt.Add(2*time.Second),
	)
	hydrated, err := fixture.database.AcceptPromotionRequest(fixture.ctx, callback)
	if err != nil || hydrated.Status != promotion.StatusApproved || hydrated.NextReconciliationAt != nil {
		t.Fatalf("callback winner = %#v, %v", hydrated, err)
	}
	diagnostic := "Kargo Promotion reached terminal phase Succeeded before callback."
	afterLateReport, err := fixture.database.ReportPromotionCreationReconciliation(
		fixture.ctx,
		promotion.CreationReconciliationReport{
			PromotionID: callbackWinner.ID,
			ClusterName: "colo",
			KargoName:   callbackWinner.ExternalPromotionID,
			KargoUID:    *callbackWinner.KargoPromotionUID,
			Phase:       promotion.CreationReconciliationSucceeded,
			Diagnostic:  diagnostic,
			ReportedAt:  callbackWinner.CreatedAt.Add(3 * time.Second),
		},
	)
	if err != nil || afterLateReport.Status != promotion.StatusApproved || afterLateReport.Message != nil {
		t.Errorf("late terminal report = %#v, %v", afterLateReport, err)
	}

	reportWinner := createDispatchablePromotion(t, fixture, createdAt.Add(4*time.Minute))
	reportWinner = materializeOperatorPromotion(t, fixture, reportWinner, reportWinner.CreatedAt.Add(time.Second))
	terminal, err := fixture.database.ReportPromotionCreationReconciliation(
		fixture.ctx,
		promotion.CreationReconciliationReport{
			PromotionID: reportWinner.ID,
			ClusterName: "colo",
			KargoName:   reportWinner.ExternalPromotionID,
			KargoUID:    *reportWinner.KargoPromotionUID,
			Phase:       promotion.CreationReconciliationErrored,
			Diagnostic:  diagnostic,
			ReportedAt:  reportWinner.CreatedAt.Add(2 * time.Second),
		},
	)
	if err != nil || terminal.Status != promotion.StatusError {
		t.Fatalf("terminal report winner = %#v, %v", terminal, err)
	}
	lateCallback := fixture.acceptance(
		t,
		fixture.request(reportWinner.ExternalPromotionID),
		reportWinner.CreatedAt.Add(3*time.Second),
	)
	if _, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		lateCallback,
	); !errors.Is(err, promotion.ErrIdentityConflict) {
		t.Errorf("late callback error = %v, want identity conflict", err)
	}
}

func TestPromotionCreationReconciliationRejectsIdentityAndClusterConflictsIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 24, 0, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	record := createDispatchablePromotion(t, fixture, createdAt.Add(2*time.Minute))
	record = materializeOperatorPromotion(t, fixture, record, record.CreatedAt.Add(time.Second))
	diagnostic := "terminal before callback"

	report := promotion.CreationReconciliationReport{
		PromotionID: record.ID,
		ClusterName: "edge",
		KargoName:   record.ExternalPromotionID,
		KargoUID:    *record.KargoPromotionUID,
		Phase:       promotion.CreationReconciliationAborted,
		Diagnostic:  diagnostic,
		ReportedAt:  record.CreatedAt.Add(2 * time.Second),
	}
	if _, err := fixture.database.ReportPromotionCreationReconciliation(
		fixture.ctx,
		report,
	); !errors.Is(err, promotion.ErrPromotionNotFound) {
		t.Errorf("wrong cluster error = %v", err)
	}
	report.ClusterName = "colo"
	report.KargoUID = "replacement-uid"
	if _, err := fixture.database.ReportPromotionCreationReconciliation(
		fixture.ctx,
		report,
	); !errors.Is(err, promotion.ErrCreationReconciliationConflict) {
		t.Errorf("wrong UID error = %v", err)
	}
	report.KargoUID = *record.KargoPromotionUID
	report.KargoName = "noncanonical-name"
	if _, err := fixture.database.ReportPromotionCreationReconciliation(
		fixture.ctx,
		report,
	); !errors.Is(err, promotion.ErrCreationReconciliationConflict) {
		t.Errorf("wrong canonical name error = %v", err)
	}
}

func TestConcurrentPromotionCreationReconciliationClaimsReturnOneCommandIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 24, 1, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	record := createDispatchablePromotion(t, fixture, createdAt.Add(2*time.Minute))
	record = materializeOperatorPromotion(t, fixture, record, record.CreatedAt.Add(time.Second))
	claim := promotion.CreationReconciliationClaim{
		ClusterName: "colo",
		ClaimedAt:   *record.NextReconciliationAt,
		Interval:    15 * time.Second,
	}

	type result struct {
		record promotion.Promotion
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			claimed, err := fixture.database.ClaimPromotionCreationReconciliation(fixture.ctx, claim)
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
		case errors.Is(result.err, promotion.ErrNoCreationReconciliation):
			noWorkCount++
		default:
			t.Fatalf("concurrent reconciliation claim = %#v, %v", result.record, result.err)
		}
	}
	if claimedCount != 1 || noWorkCount != 1 {
		t.Errorf("concurrent reconciliation results: claimed = %d, no work = %d", claimedCount, noWorkCount)
	}
}

func materializeOperatorPromotion(
	t *testing.T,
	fixture promotionIntegrationFixture,
	record promotion.Promotion,
	reportedAt time.Time,
) promotion.Promotion {
	t.Helper()

	claimed, err := fixture.database.ClaimPromotionCreation(fixture.ctx, promotion.CreationDispatchClaim{
		ClusterName:      "colo",
		ClaimedAt:        record.CreatedAt,
		RetryDelays:      []time.Duration{time.Minute},
		ExhaustedMessage: "dispatch attempts exhausted",
	})
	if err != nil {
		t.Fatalf("claim Promotion creation: %v", err)
	}
	kargoName := "external-dns-prod." + record.ID.String() + ".freight"
	kargoUID := "uid-" + record.ID.String()
	materialized, err := fixture.database.ReportPromotionCreation(
		fixture.ctx,
		promotion.CreationDispatchReport{
			PromotionID: record.ID,
			ClusterName: "colo",
			Attempt:     claimed.DispatchAttemptCount,
			Result:      promotion.CreationDispatchCreated,
			KargoName:   &kargoName,
			KargoUID:    &kargoUID,
			ReportedAt:  reportedAt,
		},
	)
	if err != nil {
		t.Fatalf("report materialized Promotion: %v", err)
	}

	return materialized
}

func createDispatchablePromotion(
	t *testing.T,
	fixture promotionIntegrationFixture,
	acceptedAt time.Time,
) promotion.Promotion {
	t.Helper()

	stage := fixture.stage
	stage.ResourceVersion = uuid.NewString()
	stage.Freight = promotion.Freight{Items: []promotion.FreightItem{{
		Origin:    "Warehouse/external-dns",
		Name:      "freight-42",
		Artifacts: []promotion.FreightArtifact{{Kind: "image", Version: "1.2.3"}},
	}}}
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
