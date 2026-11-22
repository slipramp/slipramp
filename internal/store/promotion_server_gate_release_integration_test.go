package store

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func fixedGateClock(at time.Time) gateCheckClock {
	return func(*storedb.Queries, context.Context) (pgtype.Timestamptz, error) { return databaseTime(at), nil }
}

func (d *Database) releasePromotionAt(ctx context.Context, id, process uuid.UUID, at time.Time) (bool, error) {
	status, err := d.releasePromotion(ctx, id, process, fixedGateClock(at))
	return status == promotion.KargoResponseStatus(promotion.LifecycleApproved), err
}

func (d *Database) claimPromotionCreationAt(ctx context.Context, claim promotion.CreationDispatchClaim) (promotion.Promotion, error) {
	return d.claimPromotionCreation(ctx, claim, fixedGateClock(claim.ClaimedAt))
}

func TestPromotionReleaseComposesAllFiveGatesIntegration(t *testing.T) {
	f, automatic, claim := gatedPromotionCreation(t)
	db, ctx, process := f.database, f.ctx, claim.GateEvaluatorProcessID
	callback, err := db.AcceptPromotionRequest(ctx, f.acceptance(t, f.request("callback-release"), claim.ClaimedAt))
	if err != nil {
		t.Fatal(err)
	}
	claim.RetryDelays = []time.Duration{time.Microsecond, time.Minute}
	next := func() (promotion.Promotion, error) {
		claim.ClaimedAt = time.Now().UTC()
		return db.ClaimPromotionCreation(ctx, claim)
	}
	poll := func(id uuid.UUID, want promotion.KargoResponseStatus) {
		t.Helper()
		got, err := db.ReleasePromotion(ctx, id, process)
		if err != nil || got != want {
			t.Fatalf("callback %s = %s, %v, want %s", id, got, err, want)
		}
	}
	owners := []gate.Scope{{Kind: gate.GlobalScope}, {Kind: gate.StageScope, ID: f.stage.ID}}
	closed := closedGateTestWindow(time.Now().UTC())
	setGateReleasePolicies(t, f, closed, owners...)
	if _, err := db.ChangeServerGateLock(ctx, gate.Scope{Kind: gate.TargetScope, ID: activeTargetID(t, db, f.repositoryID, "network")}, f.actor,
		&gate.LockRequest{Reason: "Target only"}); err != nil {
		t.Fatal(err)
	}
	service := promotion.NewReviewService(nil, db, process, nil)
	summaries := func() map[uuid.UUID]gate.Summary {
		t.Helper()
		items, err := service.GateSummaries(ctx, []promotion.Promotion{automatic, callback}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	names := []string{"global-lock", "stage-lock", "global-window", "stage-window", "network-idle"}
	for _, id := range []uuid.UUID{automatic.ID, callback.ID} {
		checks := summaries()[id]
		if len(checks) != len(names) || checks.State() != gate.SummaryWaiting {
			t.Fatalf("live Gates = %#v", checks)
		}
		for i, check := range checks {
			if check.Name != names[i] || (i < 4 && (check.State != gate.RequirementBlocking || check.Origin != gate.ServerOrigin)) {
				t.Fatalf("live Gate = %#v", check)
			}
		}
	}
	notDispatched := func() {
		t.Helper()
		if _, err := next(); !errors.Is(err, promotion.ErrNoCreationDispatch) {
			t.Fatalf("blocked creation claim = %v", err)
		}
	}
	overrideReleaseGates(t, f, gate.PromotionSubject, callback.ID, process, len(names), func() {
		poll(callback.ID, promotion.KargoResponseStatusGated)
		notDispatched()
	})
	approved := promotion.KargoResponseStatus(promotion.LifecycleApproved)
	poll(callback.ID, approved)
	overrideReleaseGates(t, f, gate.PromotionSubject, automatic.ID, process, len(names), notDispatched)
	first, err := next()
	if err != nil || first.ID != automatic.ID || first.CreationDispatch.AttemptCount != 1 {
		t.Fatalf("first dispatch = %#v, %v", first, err)
	}
	evidence := func(id uuid.UUID) []byte {
		t.Helper()
		var data []byte
		var repoAt time.Time
		err := db.pool.QueryRow(ctx, `SELECT p.server_gate_release_evidence,g.released_at FROM promotions p
			JOIN promotion_gates g ON g.promotion_id=p.id WHERE p.id=$1`, id).Scan(&data, &repoAt)
		if err != nil {
			t.Fatal(err)
		}
		requireGateReleaseEvidence(t, data, repoAt, f.actor, owners)
		if id == automatic.ID && !repoAt.Equal(*first.CreationDispatch.LastAttemptAt) {
			t.Fatal("first dispatch time differs from release")
		}
		return data
	}
	beforeCallback, beforeDispatch := evidence(callback.ID), evidence(automatic.ID)
	for _, owner := range owners {
		if _, err := db.ChangeServerGateWindow(ctx, owner, f.actor, &closed); err != nil {
			t.Fatal(err)
		}
	}
	poll(callback.ID, approved)
	poll(automatic.ID, approved)
	again, err := next()
	if err != nil || again.ID != automatic.ID || again.CreationDispatch.AttemptCount != 2 {
		t.Fatalf("retry = %#v, %v", again, err)
	}
	releasedSummaries := summaries()
	for _, id := range []uuid.UUID{automatic.ID, callback.ID} {
		checks := releasedSummaries[id]
		if len(checks) != len(names) || checks.State() != gate.SummaryReleased {
			t.Fatalf("release checks = %#v", checks)
		}
		for i, check := range checks {
			if check.Name != names[i] || check.ReleasedAt == nil || len(check.Overrides) != 1 || check.Overrides[0].Reference != check.Reference {
				t.Fatalf("released Gate %d = %#v", i, check)
			}
		}
	}
	if !bytes.Equal(beforeCallback, evidence(callback.ID)) || !bytes.Equal(beforeDispatch, evidence(automatic.ID)) {
		t.Fatal("repeat delivery or callback rewrote release evidence")
	}
}

func TestPromotionReviewAndCreationProceedUnderStageLockIntegration(t *testing.T) {
	at := time.Now().UTC().Add(-3 * time.Minute)
	f := newPromotionIntegrationFixture(t, at)
	ctx, db, process := f.ctx, f.database, uuid.New()
	scope := gate.Scope{Kind: gate.StageScope, ID: f.stage.ID}
	if _, err := db.ChangeServerGateLock(ctx, scope, f.actor, &gate.LockRequest{Reason: "maintenance"}); err != nil {
		t.Fatal(err)
	}
	pending := preparePromotionForReview(t, f, "waiting-for-review", at.Add(2*time.Minute))
	record, _, err := db.DecidePromotion(ctx, promotion.Decision{
		PromotionID: pending.ID, Decision: promotion.DecisionApproved, DecidedByPrincipalID: f.principal.ID,
		DecidedBySnapshot: f.actor, DecidedAt: time.Now().UTC(),
	})
	if err != nil || record.Lifecycle.State != promotion.LifecycleApproved {
		t.Fatalf("review = %#v, %v", record, err)
	}
	if got, err := db.ReleasePromotion(ctx, record.ID, process); err != nil || got != promotion.KargoResponseStatusGated {
		t.Fatalf("approved but locked callback = %s, %v", got, err)
	}

	stage := f.stage
	stage.ResourceVersion = "review-delivery"
	stage.Freight = promotion.Freight{Items: []promotion.FreightItem{{Origin: "Warehouse/external-dns", Name: "freight-42", Artifacts: []promotion.FreightArtifact{{Kind: "image", Version: "1"}}}}}
	stage.LastObservedAt = time.Now().UTC()
	stage.FirstObservedAt = stage.LastObservedAt
	if err := db.ReplacePromotionStageSnapshot(ctx, promotionTestSnapshot(uuid.New(), stage.Integration, stage.ClusterName, stage.LastObservedAt), []promotion.Stage{stage}); err != nil {
		t.Fatal(err)
	}
	native, err := db.CreateOperatorPromotion(ctx, promotion.OperatorPromotionAcceptance{
		ID: uuid.New(), Integration: stage.Integration, SourceStageID: stage.ID, TargetStageID: stage.ID, FreightName: "freight-42",
		AllowedRepositories: []string{stage.RepositoryName}, RequestedByPrincipalID: f.principal.ID, RequestedBySnapshot: f.actor,
		AcceptedAt: time.Now().UTC(), MaximumStageObservationAge: 5 * time.Minute, PendingReviewRetention: 7 * 24 * time.Hour,
	})
	if err != nil || !native.ReviewRequired {
		t.Fatalf("review-required creation = %#v, %v", native, err)
	}
	claimed, err := db.ClaimPromotionCreation(ctx, promotion.CreationDispatchClaim{ClusterName: stage.ClusterName, GateEvaluatorProcessID: process, ClaimedAt: time.Now().UTC(), RetryDelays: []time.Duration{time.Minute}, ExhaustedMessage: "exhausted"})
	if err != nil || claimed.ID != native.ID {
		t.Fatalf("review delivery under lock = %#v, %v", claimed, err)
	}
	row, err := db.queries.GetPromotion(ctx, databaseUUID(native.ID))
	if err != nil || row.CallbackReleased || row.ServerGateReleaseEvidence != nil {
		t.Fatalf("review delivery prematurely released Gates: %v", err)
	}
	if _, err := db.ChangeServerGateLock(ctx, scope, f.actor, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := db.ReleasePromotion(ctx, record.ID, process); err != nil || got != promotion.KargoResponseStatus(promotion.LifecycleApproved) {
		t.Fatalf("callback after unlock = %s, %v", got, err)
	}
}

func TestPromotionReleaseUpgradePreservesPriorBoundariesIntegration(t *testing.T) {
	at := time.Now().UTC().Add(-time.Minute)
	f := newPromotionIntegrationFixture(t, at.Add(-3*time.Minute))
	db, ctx, process := f.database, f.ctx, uuid.New()
	callback, err := db.AcceptPromotionRequest(ctx, f.acceptance(t, f.request("older-callback"), at))
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := db.AcceptPromotionRequest(ctx, f.acceptance(t, f.request("waiting-callback"), at))
	if err != nil {
		t.Fatal(err)
	}
	automatic := createDispatchablePromotion(t, f, at)
	approved := promotion.KargoResponseStatus(promotion.LifecycleApproved)
	if got, err := db.ReleasePromotion(ctx, callback.ID, process); err != nil || got != approved {
		t.Fatalf("initial callback = %s, %v", got, err)
	}
	claim := promotion.CreationDispatchClaim{ClusterName: f.stage.ClusterName, GateEvaluatorProcessID: process, ClaimedAt: time.Now().UTC(),
		RetryDelays: []time.Duration{time.Microsecond, time.Minute}, ExhaustedMessage: "exhausted"}
	if got, err := db.ClaimPromotionCreation(ctx, claim); err != nil || got.ID != automatic.ID {
		t.Fatalf("initial dispatch = %s, %v", got.ID, err)
	}

	files, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	adapter := stdlib.OpenDBFromPool(db.pool)
	defer func() { _ = adapter.Close() }()
	migrations, err := goose.NewProvider(goose.DialectPostgres, adapter, files, goose.WithTableName(migrationTable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrations.DownTo(ctx, 79); err != nil {
		t.Fatal(err)
	}
	if _, err = migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ChangeServerGateLock(ctx, gate.Scope{Kind: gate.GlobalScope}, f.actor, &gate.LockRequest{Reason: "new hold"}); err != nil {
		t.Fatal(err)
	}
	if got, err := db.ReleasePromotion(ctx, waiting.ID, process); err != nil || got != promotion.KargoResponseStatusGated {
		t.Fatalf("historical waiter = %s, %v", got, err)
	}
	for _, id := range []uuid.UUID{callback.ID, automatic.ID} {
		if got, err := db.ReleasePromotion(ctx, id, process); err != nil || got != approved {
			t.Fatalf("historical release = %s, %v", got, err)
		}
	}
	claim.ClaimedAt = time.Now().UTC()
	if got, err := db.ClaimPromotionCreation(ctx, claim); err != nil || got.ID != automatic.ID || got.CreationDispatch.AttemptCount != 2 {
		t.Fatalf("historical retry = %#v, %v", got, err)
	}
	rows, err := db.ListPromotionGateSummaries(ctx, []uuid.UUID{automatic.ID, callback.ID, waiting.ID}, process, time.Now().UTC())
	if err != nil || len(rows[automatic.ID]) != 0 || len(rows[callback.ID]) != 0 || len(rows[waiting.ID]) != 1 {
		t.Fatalf("historical summaries = %#v, %v", rows, err)
	}
}

func TestExternalStageObservationDoesNotCreateMediatedPromotionIntegration(t *testing.T) {
	now := time.Now().UTC()
	f := newPromotionIntegrationFixture(t, now.Add(-2*time.Minute))
	if _, err := f.database.ChangeServerGateLock(f.ctx, gate.Scope{Kind: gate.GlobalScope}, f.actor, &gate.LockRequest{Reason: "maintenance"}); err != nil {
		t.Fatal(err)
	}
	stage := f.stage
	stage.ResourceVersion = "external-promotion"
	stage.FirstObservedAt, stage.LastObservedAt = now, now
	stage.LastPromotion = &promotion.StagePromotionReference{Name: "deployed-directly", Phase: "Succeeded", FinishedAt: &now}
	if err := f.database.ReplacePromotionStageSnapshot(f.ctx, promotionTestSnapshot(uuid.New(), stage.Integration, stage.ClusterName, now), []promotion.Stage{stage}); err != nil {
		t.Fatal(err)
	}
	view, err := f.database.GetPromotionStage(f.ctx, stage.ID)
	if err != nil || view.LastPromotion == nil || view.LastPromotion.Name != stage.LastPromotion.Name {
		t.Fatalf("Stage observation = %#v, %v", view.LastPromotion, err)
	}
	var n int
	if err := f.database.pool.QueryRow(f.ctx, `SELECT count(*) FROM promotions WHERE stage_id=$1`, stage.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("mediated Promotions = %d, %v", n, err)
	}
}

func TestAutomaticPromotionDispatchSkipsBlockedPromotionIntegration(t *testing.T) {
	at := time.Now().UTC().Add(-time.Minute)
	f := newPromotionIntegrationFixture(t, at.Add(-3*time.Minute))
	ctx, db, process := f.ctx, f.database, uuid.New()
	first := createDispatchablePromotion(t, f, at)
	second := mustCreateOperatorPromotion(t, f, f.stage.ID, at.Add(time.Second))
	hold, err := db.ChangeServerGateLock(ctx, gate.Scope{Kind: gate.GlobalScope}, f.actor, &gate.LockRequest{Reason: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	always := openGateTestWindow()
	_, err = db.ChangeServerGateWindow(ctx, gate.Scope{Kind: gate.StageScope, ID: f.stage.ID}, f.actor, &always)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := db.MetricsSnapshotSource(process).LoadMetricsSnapshot(ctx, time.Now().UTC())
	if err != nil || metrics.WaitingGateSubjects["promotion"] != 2 {
		t.Fatalf("waiting Promotions = %v, %v", metrics.WaitingGateSubjects, err)
	}
	_, err = db.RecordGateOverride(ctx, gate.OverrideRequest{Subject: gate.PromotionSubject, SubjectID: second.ID, Key: gate.LockKey(hold.ID),
		Version: "1", Actor: f.actor, RecordedAt: at, EvaluatorProcessID: process, Reason: "restore service"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.ClaimPromotionCreation(ctx, promotion.CreationDispatchClaim{ClusterName: f.stage.ClusterName, GateEvaluatorProcessID: process,
		ClaimedAt: time.Now().UTC(), RetryDelays: []time.Duration{time.Minute}, ExhaustedMessage: "exhausted"})
	if err != nil || got.ID != second.ID {
		t.Fatalf("first eligible Promotion = %s, %v", got.ID, err)
	}
	summaries, err := db.ListPromotionGateSummaries(ctx, []uuid.UUID{first.ID, second.ID}, process, time.Now().UTC())
	checks := summaries[second.ID]
	if err != nil || summaries[first.ID].State() != gate.SummaryWaiting || len(checks) != 2 || checks.State() != gate.SummaryReleased {
		t.Fatalf("dispatch summaries = %#v, %v", summaries, err)
	}
	if len(checks[0].Overrides) != 1 || checks[1].Name != "stage-window" || len(checks[1].Overrides) != 0 ||
		checks[1].Evidence.Payload.(*gate.WindowEvidence).Observation.State != gate.RequirementPassing {
		t.Fatalf("waived lock and fresh Stage window = %#v", checks)
	}
}
