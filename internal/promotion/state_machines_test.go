package promotion

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPromotionLifecycleTransitions(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	states := []LifecycleState{
		LifecycleCreating,
		LifecyclePreparing,
		LifecyclePending,
		LifecycleApproved,
		LifecycleRejected,
		LifecycleExpired,
		LifecycleError,
	}
	permitted := map[[2]LifecycleState]bool{
		{LifecycleCreating, LifecyclePreparing}: true,
		{LifecycleCreating, LifecycleApproved}:  true,
		{LifecycleCreating, LifecycleError}:     true,
		{LifecyclePreparing, LifecyclePending}:  true,
		{LifecyclePreparing, LifecycleError}:    true,
		{LifecyclePending, LifecycleApproved}:   true,
		{LifecyclePending, LifecycleRejected}:   true,
		{LifecyclePending, LifecycleExpired}:    true,
	}

	for _, from := range states {
		for _, to := range states {
			from, to := from, to
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				reviewRequired := from != LifecycleCreating || to != LifecycleApproved
				got, err := TransitionLifecycle(
					PromotionLifecycle{State: from},
					to,
					reviewRequired,
					at,
				)
				if permitted[[2]LifecycleState{from, to}] {
					if err != nil || got.State != to {
						t.Fatalf("TransitionLifecycle() = %#v, %v", got, err)
					}
					wantDecisionTime := reviewRequired && (to == LifecycleApproved || to == LifecycleRejected)
					if (got.DecidedAt != nil) != wantDecisionTime {
						t.Errorf("decision time = %v, want present %t", got.DecidedAt, wantDecisionTime)
					}
					return
				}
				if !errors.Is(err, ErrTransitionConflict) {
					t.Fatalf("TransitionLifecycle() error = %v, want conflict", err)
				}
			})
		}
	}

	pending := PromotionLifecycle{State: LifecyclePending, ExpiresAt: new(at.Add(time.Minute))}
	for _, decision := range []DecisionValue{DecisionApproved, DecisionRejected} {
		t.Run("decision_"+string(decision), func(t *testing.T) {
			next, err := TransitionDecision(pending, true, Decision{Decision: decision, DecidedAt: at})
			if err != nil || next.State != LifecycleState(decision) || next.DecidedAt == nil {
				t.Fatalf("TransitionDecision() = %#v, %v", next, err)
			}
		})
	}

	if _, changed, err := TransitionExpiration(pending, at); err != nil || changed {
		t.Fatalf("early expiration changed = %t, error = %v", changed, err)
	}
	expired, changed, err := TransitionExpiration(pending, at.Add(time.Minute))
	if err != nil || !changed || expired.State != LifecycleExpired {
		t.Fatalf("expiration = %#v, changed %t, %v", expired, changed, err)
	}
}

func TestCreationDispatchTransitions(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	claim := CreationDispatchClaim{
		ClusterName: "colo",
		ClaimedAt:   at,
		RetryDelays: []time.Duration{time.Second, 2 * time.Second},
	}
	baseState := CreationDispatchState{NextAttemptAt: new(at)}

	tests := []struct {
		name          string
		state         CreationDispatchState
		wantAttempt   int
		wantExhausted bool
	}{
		{name: "first attempt", state: baseState, wantAttempt: 1},
		{
			name: "retry",
			state: CreationDispatchState{
				AttemptCount: 1, NextAttemptAt: new(at), LastAttemptAt: new(at.Add(-time.Second)),
			},
			wantAttempt: 2,
		},
		{
			name: "exhaustion",
			state: CreationDispatchState{
				AttemptCount: 2, NextAttemptAt: new(at), LastAttemptAt: new(at.Add(-time.Second)),
			},
			wantAttempt: 2, wantExhausted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TransitionCreationDispatchClaim(
				SourceSlipramp,
				PromotionLifecycle{State: LifecycleCreating},
				test.state,
				claim,
			)
			if err != nil || got.State.AttemptCount != test.wantAttempt || got.Exhausted != test.wantExhausted {
				t.Fatalf("TransitionCreationDispatchClaim() = %#v, %v", got, err)
			}
			if test.wantExhausted && got.Lifecycle.State != LifecycleError {
				t.Errorf("exhausted lifecycle = %q", got.Lifecycle.State)
			}
		})
	}

	promotionID := uuid.New()
	kargoName := "stage.slipramp.freight"
	kargoUID := "promotion-uid"
	diagnostic := "temporary Kubernetes failure"
	current := Promotion{
		ID: promotionID, Source: SourceSlipramp, ExternalPromotionID: "stage-slipramp-" + promotionID.String(),
		ReviewRequired: true,
		Lifecycle:      PromotionLifecycle{State: LifecycleCreating},
		CreationDispatch: CreationDispatchState{
			AttemptCount: 2, NextAttemptAt: new(at.Add(time.Minute)), LastAttemptAt: new(at),
		},
		UpdatedAt: at,
	}

	retryReport := CreationDispatchReport{
		PromotionID: promotionID, Attempt: 2, Result: CreationDispatchRetryable,
		Diagnostic: &diagnostic, ReportedAt: at.Add(time.Second),
	}
	retrying, changed, err := TransitionCreationDispatchReport(current, retryReport)
	if err != nil || !changed || retrying.CreationDispatch.Diagnostic == nil ||
		*retrying.CreationDispatch.Diagnostic != diagnostic || retrying.Message != nil {
		t.Fatalf("retry report = %#v, changed %t, %v", retrying, changed, err)
	}
	lateRetry := retryReport
	lateRetry.Attempt = 1
	if _, changed, err := TransitionCreationDispatchReport(current, lateRetry); err != nil || changed {
		t.Fatalf("late retry changed = %t, error = %v", changed, err)
	}

	createdReport := CreationDispatchReport{
		PromotionID: promotionID, Attempt: 1, Result: CreationDispatchCreated,
		KargoName: &kargoName, KargoUID: &kargoUID, ReportedAt: at.Add(2 * time.Second),
	}
	created, changed, err := TransitionCreationDispatchReport(current, createdReport)
	if err != nil || !changed || created.CreationDispatch.DispatchedAt == nil ||
		created.KargoExecution.PromotionUID == nil || created.KargoExecution.NextReconciliationAt == nil {
		t.Fatalf("created report = %#v, changed %t, %v", created, changed, err)
	}
	if _, changed, err := TransitionCreationDispatchReport(created, createdReport); err != nil || changed {
		t.Fatalf("duplicate created report changed = %t, error = %v", changed, err)
	}

	failedReport := retryReport
	failedReport.Result = CreationDispatchFailed
	failed, changed, err := TransitionCreationDispatchReport(current, failedReport)
	if err != nil || !changed || failed.Lifecycle.State != LifecycleError ||
		failed.CreationDispatch.NextAttemptAt != nil || failed.CreationDispatch.FailedAt == nil {
		t.Fatalf("failed report = %#v, changed %t, %v", failed, changed, err)
	}
	lateCreated, changed, err := TransitionCreationDispatchReport(failed, createdReport)
	if err != nil || changed || lateCreated.Lifecycle.State != LifecycleError ||
		lateCreated.KargoExecution.PromotionUID != nil || lateCreated.KargoExecution.NextReconciliationAt != nil {
		t.Fatalf("late created report = %#v, changed %t, %v", lateCreated, changed, err)
	}
}

func TestPreparationTransitions(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	lifecycle := PromotionLifecycle{State: LifecyclePreparing, ExpiresAt: new(at.Add(time.Hour))}
	repositoryID := uuid.New()
	revisionID := uuid.New()
	runID := uuid.New()

	reserved, changed, err := TransitionPreparationReservation(
		lifecycle, true, PreparationState{}, repositoryID, revisionID,
	)
	if err != nil || !changed || reserved.RepositoryID == nil || reserved.CatalogRevisionID == nil {
		t.Fatalf("reservation = %#v, changed %t, %v", reserved, changed, err)
	}
	if _, changed, err := TransitionPreparationReservation(
		lifecycle, true, reserved, repositoryID, revisionID,
	); err != nil || changed {
		t.Fatalf("duplicate reservation changed = %t, error = %v", changed, err)
	}
	linked, err := TransitionPreparationRunLink(lifecycle, true, reserved, repositoryID, revisionID, runID)
	if err != nil || linked.RunID == nil || *linked.RunID != runID {
		t.Fatalf("Run link = %#v, %v", linked, err)
	}

	diagnostic := "dependency unavailable"
	failure := PreparationFailure{
		PromotionID: uuid.New(), PreparationRunID: &runID, FailedAt: at, Diagnostic: diagnostic,
	}
	retrying, err := TransitionPreparationFailure(lifecycle, true, linked, failure)
	if err != nil || !retrying.Changed || retrying.State.FailureCount != 1 || retrying.State.NextAttemptAt == nil ||
		retrying.Lifecycle.State != LifecyclePreparing {
		t.Fatalf("transient failure = %#v, %v", retrying, err)
	}
	duplicate, err := TransitionPreparationFailure(lifecycle, true, retrying.State, failure)
	if err != nil || duplicate.Changed || duplicate.State.FailureCount != 1 {
		t.Fatalf("duplicate failure = %#v, %v", duplicate, err)
	}

	permanentFailure := failure
	permanentFailure.Permanent = true
	permanentFailure.FailedAt = at.Add(time.Minute)
	failed, err := TransitionPreparationFailure(lifecycle, true, linked, permanentFailure)
	if err != nil || !failed.Changed || failed.Lifecycle.State != LifecycleError || failed.State.CompletedAt == nil {
		t.Fatalf("permanent failure = %#v, %v", failed, err)
	}
	gitArtifactID := uuid.New()
	argoArtifactID := uuid.New()
	pending, completed, err := TransitionPreparationCompletion(
		lifecycle,
		true,
		linked,
		runID,
		gitArtifactID,
		argoArtifactID,
		at.Add(2*time.Minute),
	)
	if err != nil || pending.State != LifecyclePending || completed.GitDiffArtifactID == nil ||
		completed.ArgoDiffArtifactID == nil || completed.CompletedAt == nil {
		t.Fatalf("completion = %#v, %#v, %v", pending, completed, err)
	}
}

func TestKargoExecutionTransitions(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	name := "external-dns.01test.freight"
	uid := "promotion-uid"
	base := Promotion{
		ID: uuid.New(), Source: SourceKargo, ExternalPromotionID: name,
		StageSnapshot:  PromotionStageSnapshot{ClusterName: "colo"},
		Lifecycle:      PromotionLifecycle{State: LifecycleApproved},
		KargoExecution: KargoExecutionState{NextReconciliationAt: new(at)},
		CreatedAt:      at.Add(-time.Minute), UpdatedAt: at.Add(-time.Minute),
	}
	approved := validPromotion()
	approved.Lifecycle.State = LifecycleApproved
	approved.Lifecycle.DecidedAt = new(at)
	approved.KargoExecution.NextReconciliationAt = new(at)
	approved.UpdatedAt = at
	if err := approved.Validate(); err != nil || approved.KargoExecution.Phase() != KargoExecutionReconciling {
		t.Fatalf("approved and executing Promotion = %#v, %v", approved, err)
	}

	claimed, err := TransitionReconciliationClaim(
		base.Source,
		base.Lifecycle,
		base.KargoExecution,
		ReconciliationClaim{ClaimedAt: at, Interval: 15 * time.Second},
	)
	if err != nil || claimed.LastReconciliationAt == nil ||
		claimed.NextReconciliationAt == nil || !claimed.NextReconciliationAt.Equal(at.Add(15*time.Second)) {
		t.Fatalf("reconciliation claim = %#v, %v", claimed, err)
	}

	identityReport := ReconciliationReport{
		PromotionID: base.ID, ClusterName: "colo", KargoName: name, KargoUID: uid, ObservedAt: at,
	}
	bound, err := TransitionReconciliationReport(base, identityReport)
	if err != nil || !bound.IdentityBound || bound.OutcomeRecorded ||
		bound.Promotion.KargoExecution.PromotionUID == nil {
		t.Fatalf("identity report = %#v, %v", bound, err)
	}
	duplicate, err := TransitionReconciliationReport(bound.Promotion, identityReport)
	if err != nil || duplicate.IdentityBound || duplicate.OutcomeRecorded {
		t.Fatalf("duplicate identity report = %#v, %v", duplicate, err)
	}

	phase := KargoPhaseSucceeded
	terminalReport := identityReport
	terminalReport.Phase = &phase
	terminal, err := TransitionReconciliationReport(bound.Promotion, terminalReport)
	if err != nil || !terminal.OutcomeRecorded || terminal.Promotion.KargoExecution.Outcome == nil ||
		terminal.Promotion.KargoExecution.NextReconciliationAt != nil {
		t.Fatalf("terminal report = %#v, %v", terminal, err)
	}
	repeated, err := TransitionReconciliationReport(terminal.Promotion, terminalReport)
	if err != nil || repeated.OutcomeRecorded {
		t.Fatalf("duplicate terminal report = %#v, %v", repeated, err)
	}

	conflictingIdentity := identityReport
	conflictingIdentity.KargoUID = "other-uid"
	if _, err := TransitionReconciliationReport(bound.Promotion, conflictingIdentity); !errors.Is(err, ErrReconciliationConflict) {
		t.Fatalf("conflicting identity error = %v", err)
	}
}
