package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestRunReviewGitTriggerLifecycleIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	baseTime := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	repositoryID := uuid.New()
	if _, err := database.CreateRepository(ctx, repository.Repository{
		ID: repositoryID, Name: "run-reviews", CloneURL: "https://github.com/example/run-reviews.git",
		Branch: "main", CommitIndexing: repository.CommitIndexingObservedHeads, CreatedAt: baseTime,
	}); err != nil {
		t.Fatalf("create review repository: %v", err)
	}

	operator := createRunReviewPrincipal(t, database, "Review operator", baseTime)
	otherOperator := createRunReviewPrincipal(t, database, "Other operator", baseTime)
	operatorActor, err := operator.ActorSnapshot()
	if err != nil {
		t.Fatalf("snapshot review operator: %v", err)
	}
	otherActor, err := otherOperator.ActorSnapshot()
	if err != nil {
		t.Fatalf("snapshot other review operator: %v", err)
	}

	reviewCatalog := gitTriggeredCatalogWithReview()
	recordRunReviewHead(t, database, repositoryID, "a", baseTime.Add(time.Minute), reviewCatalog, false)
	runs, dispatcher := newGitTriggerTestServices(t, database)

	// A review-required delivery freezes that requirement onto a pending Run.
	recordRunReviewHead(t, database, repositoryID, "b", baseTime.Add(2*time.Minute), reviewCatalog, true)
	delivery, err := database.GetEligibleGitTriggerDelivery(ctx, time.Now())
	if err != nil {
		t.Fatalf("get review-required delivery: %v", err)
	}
	if !delivery.ReviewRequired {
		t.Fatal("Git-trigger delivery did not snapshot its review requirement")
	}
	if processed, err := dispatcher.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("dispatch review-required Run = (%t, %v)", processed, err)
	}
	pending := onlyRepositoryRun(t, database, repositoryID)
	if pending.Phase != run.PhaseQueued || pending.Review.State != run.ReviewStatePending {
		t.Fatalf("pending review Run = %#v", pending)
	}
	if _, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(3*time.Minute)); !errors.Is(err, run.ErrNoRunAssignment) {
		t.Fatalf("claim pending-review Run error = %v, want no assignment", err)
	}

	// The older pending-review Run does not block a later eligible Run for the
	// same Target.
	manual := queuedTestRun(
		uuid.New(), pending.TargetID, repositoryID, pending.CatalogRevisionID,
		pending.CommitSHA, run.SourceTypeAPI, baseTime.Add(4*time.Minute), "1",
	)
	if _, err := database.CreateRun(ctx, manual); err != nil {
		t.Fatalf("create later eligible Run: %v", err)
	}
	claimedManual, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("claim later eligible Run: %v", err)
	}
	if claimedManual.ID != manual.ID || claimedManual.Review.State != run.ReviewStateNotRequired {
		t.Fatalf("claimed Run = %#v, want later direct Run", claimedManual)
	}
	if _, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: claimedManual.ID, Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(6 * time.Minute),
	}); !errors.Is(err, run.ErrReviewConflict) {
		t.Fatalf("decision for assigned direct Run error = %v, want conflict", err)
	}
	finishAssignedRun(t, database, claimedManual, baseTime.Add(6*time.Minute))

	// Approval keeps the existing Run queued and restores ordinary assignment
	// eligibility. An identical queued retry is idempotent; actor or decision
	// changes conflict.
	approved, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: pending.ID, Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(7 * time.Minute),
	})
	if err != nil {
		t.Fatalf("approve pending Run: %v", err)
	}
	if approved.Phase != run.PhaseQueued || approved.Review.State != run.ReviewStateApproved ||
		approved.Review.DecidedBy == nil || approved.Review.DecidedBy.PrincipalID != operator.ID {
		t.Fatalf("approved Run = %#v", approved)
	}
	repeatedApproval, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: pending.ID, Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(8 * time.Minute),
	})
	if err != nil || repeatedApproval.Review.DecidedAt == nil || approved.Review.DecidedAt == nil ||
		!repeatedApproval.Review.DecidedAt.Equal(*approved.Review.DecidedAt) {
		t.Fatalf("repeat approval = (%#v, %v)", repeatedApproval, err)
	}
	for _, transition := range []run.ReviewTransition{
		{
			RunID: pending.ID, Decision: run.ReviewDecisionRejected,
			DecidedBy: operatorActor, DecidedAt: baseTime.Add(8 * time.Minute),
		},
		{
			RunID: pending.ID, Decision: run.ReviewDecisionApproved,
			DecidedBy: otherActor, DecidedAt: baseTime.Add(8 * time.Minute),
		},
	} {
		if _, err := database.DecideRunReview(ctx, transition); !errors.Is(err, run.ErrReviewConflict) {
			t.Errorf("conflicting queued decision error = %v, want conflict", err)
		}
	}
	claimedApproved, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(9*time.Minute))
	if err != nil || claimedApproved.ID != pending.ID {
		t.Fatalf("claim approved Run = (%#v, %v)", claimedApproved, err)
	}
	if _, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: pending.ID, Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(10 * time.Minute),
	}); !errors.Is(err, run.ErrReviewConflict) {
		t.Fatalf("approval after assignment error = %v, want conflict", err)
	}
	finishAssignedRun(t, database, claimedApproved, baseTime.Add(10*time.Minute))

	// Rejection atomically records both the immutable review decision and the
	// terminal canceled lifecycle transition.
	rejectedRun := dispatchRunReviewHead(
		t, database, dispatcher, repositoryID, "c", baseTime.Add(11*time.Minute), reviewCatalog,
	)
	rejected, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: rejectedRun.ID, Decision: run.ReviewDecisionRejected,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(12 * time.Minute),
	})
	if err != nil {
		t.Fatalf("reject pending Run: %v", err)
	}
	if rejected.Phase != run.PhaseFinished || rejected.Outcome == nil ||
		*rejected.Outcome != run.OutcomeCanceled || rejected.OutcomeReason == nil ||
		*rejected.OutcomeReason != run.ReasonReviewRejected ||
		rejected.Review.State != run.ReviewStateRejected {
		t.Fatalf("rejected Run = %#v", rejected)
	}
	assertRunReviewTransition(t, database, rejected.ID, run.ReasonReviewRejected)
	if _, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: rejected.ID, Decision: run.ReviewDecisionRejected,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(13 * time.Minute),
	}); err != nil {
		t.Fatalf("repeat identical rejection: %v", err)
	}
	if _, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: rejected.ID, Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(13 * time.Minute),
	}); !errors.Is(err, run.ErrReviewConflict) {
		t.Fatalf("conflicting terminal decision error = %v, want conflict", err)
	}

	// Reruns are ordinary manual requests and do not inherit review state.
	rerunRecord, err := runs.RerunRun(ctx, run.RerunRequest{
		RunID: rejected.ID, Mode: run.RerunModeExact,
		Source: run.Source{Type: run.SourceTypeManual}, Actor: &operator,
	})
	if err != nil {
		t.Fatalf("rerun rejected Run: %v", err)
	}
	if rerunRecord.Review.State != run.ReviewStateNotRequired || rerunRecord.Source.Type != run.SourceTypeManual {
		t.Fatalf("rerun review/source = (%#v, %#v)", rerunRecord.Review, rerunRecord.Source)
	}
	if _, err := runs.CancelRun(ctx, run.CancelRequest{RunID: rerunRecord.ID, Actor: &operator}); err != nil {
		t.Fatalf("cancel rerun: %v", err)
	}

	// Ordinary cancellation retains pending review state and uses the ordinary
	// cancellation reason.
	cancelPending := dispatchRunReviewHead(
		t, database, dispatcher, repositoryID, "d", baseTime.Add(14*time.Minute), reviewCatalog,
	)
	canceled, err := runs.CancelRun(ctx, run.CancelRequest{RunID: cancelPending.ID, Actor: &operator})
	if err != nil {
		t.Fatalf("cancel pending-review Run: %v", err)
	}
	if canceled.Review.State != run.ReviewStatePending || canceled.OutcomeReason == nil ||
		*canceled.OutcomeReason != run.ReasonCancelRequested {
		t.Fatalf("canceled pending-review Run = %#v", canceled)
	}
	if _, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: canceled.ID, Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(15 * time.Minute),
	}); !errors.Is(err, run.ErrReviewConflict) {
		t.Fatalf("decision after cancellation error = %v, want conflict", err)
	}

	// Latest-wins supersedes both pending and approved-but-still-queued Git Runs.
	pendingSuperseded := dispatchRunReviewHead(
		t, database, dispatcher, repositoryID, "e", baseTime.Add(16*time.Minute), reviewCatalog,
	)
	recordRunReviewHead(t, database, repositoryID, "f", baseTime.Add(17*time.Minute), reviewCatalog, true)
	assertSupersededReviewRun(t, database, pendingSuperseded.ID, run.ReviewStatePending)
	approvedSuperseded := dispatchPendingRunReviewDelivery(t, database, dispatcher, repositoryID)
	if _, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: approvedSuperseded.ID, Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(18 * time.Minute),
	}); err != nil {
		t.Fatalf("approve Run before supersession: %v", err)
	}
	recordRunReviewHead(t, database, repositoryID, "0", baseTime.Add(19*time.Minute), reviewCatalog, true)
	assertSupersededReviewRun(t, database, approvedSuperseded.ID, run.ReviewStateApproved)
	if _, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: approvedSuperseded.ID, Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(20 * time.Minute),
	}); !errors.Is(err, run.ErrReviewConflict) {
		t.Fatalf("decision after supersession error = %v, want conflict", err)
	}

	// A later trigger that omits review remains directly assignable. Advance
	// once more before dispatch so the still-pending review delivery is
	// coalesced at the delivery boundary.
	recordRunReviewHead(t, database, repositoryID, "1", baseTime.Add(21*time.Minute), gitTriggeredCatalog(), true)
	direct := dispatchPendingRunReviewDelivery(t, database, dispatcher, repositoryID)
	if direct.Review.State != run.ReviewStateNotRequired {
		t.Fatalf("direct Git Run review = %#v", direct.Review)
	}
	claimedDirect, err := database.ClaimQueuedRun(ctx, "colo", baseTime.Add(22*time.Minute))
	if err != nil || claimedDirect.ID != direct.ID {
		t.Fatalf("claim direct Git Run = (%#v, %v)", claimedDirect, err)
	}

	if _, err := database.DecideRunReview(ctx, run.ReviewTransition{
		RunID: uuid.New(), Decision: run.ReviewDecisionApproved,
		DecidedBy: operatorActor, DecidedAt: baseTime.Add(23 * time.Minute),
	}); !errors.Is(err, run.ErrRunNotFound) {
		t.Fatalf("missing Run review error = %v, want not found", err)
	}
}

func gitTriggeredCatalogWithReview() catalog.Catalog {
	result := gitTriggeredCatalog()
	target := result.Targets[0]
	action := target.Spec.Actions["sync"]
	action.Triggers[0].Review = catalog.TriggerReviewRequired
	target.Spec.Actions["sync"] = action
	result.Targets[0] = target

	return result
}

func recordRunReviewHead(
	t *testing.T,
	database *Database,
	repositoryID uuid.UUID,
	commitCharacter string,
	at time.Time,
	contents catalog.Catalog,
	changed bool,
) {
	t.Helper()

	refresh := successfulCatalogRefresh(strings.Repeat(commitCharacter, 40), at, uuid.New())
	refresh.Revision.Catalog = contents
	if changed {
		refresh.ChangedPaths = []string{"infrastructure/network/main.tf"}
	}
	if err := database.RecordRepositoryRefresh(t.Context(), repositoryID, refresh); err != nil {
		t.Fatalf("record review catalog head %q: %v", commitCharacter, err)
	}
}

func dispatchRunReviewHead(
	t *testing.T,
	database *Database,
	dispatcher interface {
		ProcessNext(context.Context) (bool, error)
	},
	repositoryID uuid.UUID,
	commitCharacter string,
	at time.Time,
	contents catalog.Catalog,
) run.Run {
	t.Helper()

	recordRunReviewHead(t, database, repositoryID, commitCharacter, at, contents, true)

	return dispatchPendingRunReviewDelivery(t, database, dispatcher, repositoryID)
}

func dispatchPendingRunReviewDelivery(
	t *testing.T,
	database *Database,
	dispatcher interface {
		ProcessNext(context.Context) (bool, error)
	},
	repositoryID uuid.UUID,
) run.Run {
	t.Helper()

	processed, err := dispatcher.ProcessNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("dispatch Git-trigger delivery = (%t, %v)", processed, err)
	}
	listed, err := database.ListRuns(t.Context(), run.RunListFilter{RepositoryID: &repositoryID, Limit: 1})
	if err != nil || len(listed.Items) != 1 {
		t.Fatalf("load dispatched Run = (%d, %v)", len(listed.Items), err)
	}

	return listed.Items[0]
}

func onlyRepositoryRun(t *testing.T, database *Database, repositoryID uuid.UUID) run.Run {
	t.Helper()

	listed, err := database.ListRuns(t.Context(), run.RunListFilter{RepositoryID: &repositoryID, Limit: 10})
	if err != nil || len(listed.Items) != 1 {
		t.Fatalf("repository Runs = (%d, %v), want one", len(listed.Items), err)
	}

	return listed.Items[0]
}

func createRunReviewPrincipal(
	t *testing.T,
	database *Database,
	displayName string,
	createdAt time.Time,
) productidentity.Principal {
	t.Helper()

	principal, err := database.CreatePrincipal(t.Context(), productidentity.Principal{
		ID: uuid.New(), Kind: productidentity.KindUser, DisplayName: displayName,
		Role: productidentity.RoleOperator, CreatedAt: createdAt, UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create Run review principal: %v", err)
	}

	return principal
}

func finishAssignedRun(t *testing.T, database *Database, record run.Run, finishedAt time.Time) {
	t.Helper()

	failed := run.OutcomeFailed
	reason := "ExecutionFailed"
	if err := database.ApplyRunObservation(t.Context(), run.Observation{
		ID: record.ID, ClusterName: record.ClusterName, Phase: run.PhaseFinished,
		Outcome: &failed, Reason: &reason, ObservedAt: finishedAt,
	}); err != nil {
		t.Fatalf("finish assigned Run %s: %v", record.ID, err)
	}
}

func assertRunReviewTransition(t *testing.T, database *Database, runID uuid.UUID, reason string) {
	t.Helper()

	var count int
	if err := database.pool.QueryRow(
		t.Context(),
		`SELECT count(*) FROM run_phase_transitions
         WHERE run_id = $1 AND phase = 'finished' AND outcome = 'canceled' AND reason = $2`,
		runID,
		reason,
	).Scan(&count); err != nil {
		t.Fatalf("count Run review transitions: %v", err)
	}
	if count != 1 {
		t.Errorf("Run review transitions = %d, want 1", count)
	}
}

func assertSupersededReviewRun(
	t *testing.T,
	database *Database,
	runID uuid.UUID,
	wantReviewState run.ReviewState,
) {
	t.Helper()

	record, err := database.GetRun(t.Context(), runID)
	if err != nil {
		t.Fatalf("get superseded review Run: %v", err)
	}
	if record.Phase != run.PhaseFinished || record.Outcome == nil ||
		*record.Outcome != run.OutcomeCanceled || record.OutcomeReason == nil ||
		*record.OutcomeReason != run.ReasonSupersededByGitChange ||
		record.Review.State != wantReviewState {
		t.Errorf("superseded review Run = %#v", record)
	}
	assertSupersededRunTransition(t, database, runID)
}
