package store

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func TestSubjectLockOverridesIntegration(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	fixture := newPromotionIntegrationFixture(t, at)
	ctx, db := fixture.ctx, fixture.database
	process := uuid.New()
	preview, err := db.GetRun(ctx, fixture.runID)
	if err != nil {
		t.Fatal(err)
	}
	createRun := func(intent catalog.ActionIntent) uuid.UUID {
		t.Helper()
		r := gateTestRun(t, db, fixture.repositoryID, preview.CatalogRevisionID, at.Add(time.Minute))
		r.Snapshot.Action.Intent = intent
		if _, err := db.CreateRun(ctx, r, nil); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	previewID := createRun(catalog.ActionIntentPreview)
	operation, reconcile := createRun(catalog.ActionIntentOperation), createRun(catalog.ActionIntentReconcile)
	record, err := db.AcceptPromotionRequest(ctx, fixture.acceptance(t, fixture.request("lock-overrides"), at.Add(2*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	global := gate.Scope{Kind: gate.GlobalScope}
	target := gate.Scope{Kind: gate.TargetScope, ID: preview.TargetID}
	stage := gate.Scope{Kind: gate.StageScope, ID: fixture.stage.ID}
	lock := func(owner gate.Scope, reason string) gate.Key {
		t.Helper()
		current, err := db.ChangeServerGateLock(ctx, owner, fixture.actor, &gate.LockRequest{Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		return gate.LockKey(current.ID)
	}
	globalKey := lock(global, "maintenance")
	targetKey := lock(target, "Target hold")
	stageKey := lock(stage, "Stage hold")
	list := func(subject gate.SubjectType, id uuid.UUID, want ...gate.Key) gate.OverrideCollection {
		t.Helper()
		got, err := db.ListGateOverrides(ctx, subject, id, process)
		if err != nil {
			t.Fatal(err)
		}
		keys := make([]gate.Key, 0, len(got.Eligible))
		for _, check := range got.Eligible {
			keys = append(keys, check.Key)
		}
		if !slices.Equal(keys, want) {
			t.Fatalf("%s override candidates = %v, want %v", subject, keys, want)
		}
		return got
	}
	const repoKey = gate.Key("repository/network-idle")
	list(gate.RunSubject, operation, globalKey, targetKey, repoKey)
	list(gate.RunSubject, reconcile, globalKey, targetKey, repoKey)
	list(gate.RunSubject, previewID, repoKey)
	list(gate.PromotionSubject, record.ID, globalKey, stageKey)
	request := gate.OverrideRequest{Subject: gate.RunSubject, SubjectID: operation, Key: globalKey, Version: "1",
		Actor: fixture.actor, RecordedAt: time.Now().UTC(), EvaluatorProcessID: process}
	reject := func(r gate.OverrideRequest) {
		t.Helper()
		if _, err := db.RecordGateOverride(ctx, r); !errors.Is(err, gate.ErrOverrideConflict) {
			t.Fatalf("accepted inapplicable override %#v: %v", r, err)
		}
	}
	wrong := request
	wrong.SubjectID = previewID
	reject(wrong)
	wrong = request
	wrong.Key = stageKey
	reject(wrong)

	// Simultaneous retries record one decision and leave the scope active for other subjects.
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := db.RecordGateOverride(ctx, request); results <- err }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	wrong = request
	wrong.Reason = "different decision"
	reject(wrong)
	history := list(gate.RunSubject, operation, targetKey, repoKey).Decisions
	if len(history) != 1 || history[0].Reference != request.Reference || history[0].Actor != fixture.actor ||
		history[0].Reason != request.Reason || history[0].ObservedState != gate.RequirementBlocking {
		t.Fatalf("history = %#v", history)
	}
	observed, ok := history[0].ObservedEvidence.Payload.(*gate.ScopeLock)
	if !ok || observed.Reason != "maintenance" || observed.Actor != fixture.actor {
		t.Fatalf("observed lock = %#v", observed)
	}
	list(gate.RunSubject, reconcile, globalKey, targetKey, repoKey)
	promRequest := request
	promRequest.Subject, promRequest.SubjectID, promRequest.Key = gate.PromotionSubject, record.ID, stageKey
	response, err := db.RecordGateOverride(ctx, promRequest)
	if err != nil || response.Origin != gate.ServerOrigin || response.Scope.Kind != gate.StageScope ||
		response.Type != gate.LockType || response.Reference != promRequest.Reference || len(response.Overrides) != 1 {
		t.Fatalf("Promotion override = %#v, %v", response, err)
	}
	list(gate.PromotionSubject, record.ID, globalKey)

	if _, err := db.ChangeServerGateLock(ctx, global, fixture.actor, nil); err != nil {
		t.Fatal(err)
	}
	reject(request)
	old := list(gate.RunSubject, operation, targetKey, repoKey)
	if len(old.Decisions) != 1 {
		t.Fatal("clearing the lock erased its decision")
	}
	lock(global, "new incident")
	reject(request)
	current := list(gate.RunSubject, operation, globalKey, targetKey, repoKey)
	if current.Eligible[0].Version != "3" || len(current.Eligible[0].Overrides) != 1 || current.Eligible[0].Overrides[0].Version != "1" {
		t.Fatalf("re-locked scope = %#v", current.Eligible[0])
	}
	if _, err := db.CancelRun(ctx, operation, time.Now().UTC(), &fixture.actor); err != nil {
		t.Fatal(err)
	}
	request.Version = "3"
	reject(request)
	if history := list(gate.RunSubject, operation).Decisions; len(history) != 1 {
		t.Fatal("missing canceled Run history")
	}

	// Clearing the remaining global hold leaves the Stage waiver sufficient to release this callback.
	if _, err := db.ChangeServerGateLock(ctx, global, fixture.actor, nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.releasePromotionAt(ctx, record.ID, process, time.Now().UTC()); err != nil || !ok {
		t.Fatalf("callback release = %t, %v", ok, err)
	}
	reject(promRequest)
	if history := list(gate.PromotionSubject, record.ID).Decisions; len(history) != 1 {
		t.Fatal("missing released Promotion history")
	}
	if _, err := db.ListGateOverrides(ctx, gate.PromotionSubject, uuid.New(), process); !errors.Is(err, gate.ErrSubjectNotFound) {
		t.Fatal(err)
	}
}

func TestLockOverrideClosesAtFirstAutomaticDispatchIntegration(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	fixture := newPromotionIntegrationFixture(t, at)
	ctx, db := fixture.ctx, fixture.database
	record := createDispatchablePromotion(t, fixture, at.Add(2*time.Minute))
	state, err := db.ChangeServerGateLock(ctx, gate.Scope{Kind: gate.StageScope, ID: fixture.stage.ID}, fixture.actor, &gate.LockRequest{Reason: "Stage hold"})
	if err != nil {
		t.Fatal(err)
	}
	request := gate.OverrideRequest{Subject: gate.PromotionSubject, SubjectID: record.ID, Key: gate.LockKey(state.ID), Version: "1",
		Actor: fixture.actor, Reason: "one delivery", RecordedAt: time.Now().UTC(), EvaluatorProcessID: uuid.New()}
	if _, err := db.RecordGateOverride(ctx, request); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.claimPromotionCreationAt(ctx, promotion.CreationDispatchClaim{
		ClusterName: "colo", ClaimedAt: record.CreatedAt, RetryDelays: []time.Duration{time.Minute}, ExhaustedMessage: "exhausted",
	})
	if err != nil || claimed.ID != record.ID {
		t.Fatalf("claim = %s, %v", claimed.ID, err)
	}
	if _, err := db.RecordGateOverride(ctx, request); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("override after first dispatch = %v", err)
	}
	list, err := db.ListGateOverrides(ctx, gate.PromotionSubject, record.ID, request.EvaluatorProcessID)
	if err != nil || len(list.Eligible) != 0 || len(list.Decisions) != 1 {
		t.Fatalf("dispatched Promotion = %#v, %v", list, err)
	}
}

func TestLockOverrideRechecksScopeAfterConcurrentChangeIntegration(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	fixture := newPromotionIntegrationFixture(t, at)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	db := fixture.database
	record := createDispatchablePromotion(t, fixture, at.Add(2*time.Minute))
	scope := gate.Scope{Kind: gate.StageScope, ID: fixture.stage.ID}
	original, err := db.ChangeServerGateLock(ctx, scope, fixture.actor, &gate.LockRequest{Reason: "initial hold"})
	if err != nil {
		t.Fatal(err)
	}

	// Hold a scope mutation open while a waiver for the old version reaches PostgreSQL.
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := db.queries.WithTx(tx)
	scopes, err := q.LockServerGateScopes(ctx, storedb.LockServerGateScopesParams{
		Kind: string(scope.Kind), OwnerID: databaseUUID(scope.ID),
	})
	if err != nil || len(scopes) != 2 {
		t.Fatalf("acquire scopes = %v, %v", scopes, err)
	}
	request := gate.OverrideRequest{
		Subject: gate.PromotionSubject, SubjectID: record.ID, Key: gate.LockKey(original.ID), Version: "1",
		Actor: fixture.actor, Reason: "one release", RecordedAt: at.Add(4 * time.Minute), EvaluatorProcessID: uuid.New(),
	}
	done := make(chan error, 1)
	go func() {
		_, err := db.RecordGateOverride(ctx, request)
		done <- err
	}()
	for {
		var blocked bool
		err := db.pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))
		)`, int32(tx.Conn().PgConn().PID())).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("override finished before scope writer committed: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cleared, err := changeScopeLock(ctx, q, scopes[1], fixture.actor, nil, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	replacement := &gate.LockRequest{Reason: "replacement"}
	if _, err := changeScopeLock(ctx, q, cleared, fixture.actor, replacement, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("waiver for replaced lock = %v", err)
	}
	list, err := db.ListGateOverrides(ctx, gate.PromotionSubject, record.ID, request.EvaluatorProcessID)
	if err != nil || len(list.Decisions) != 0 || len(list.Eligible) != 1 || list.Eligible[0].Version != "3" {
		t.Fatalf("after concurrent re-lock = %#v, %v", list, err)
	}
}

func TestPromotionCallbackReleaseBackfillIntegration(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	fixture := newPromotionIntegrationFixture(t, at)
	ctx, db := fixture.ctx, fixture.database
	accept := func(name string, acceptedAt time.Time) uuid.UUID {
		t.Helper()
		record, err := db.AcceptPromotionRequest(ctx, fixture.acceptance(t, fixture.request(name), acceptedAt))
		if err != nil || record.Lifecycle.State != promotion.LifecycleApproved {
			t.Fatalf("accept %s = %#v, %v", name, record, err)
		}
		return record.ID
	}
	ungated := accept("historical-ungated", at.Add(2*time.Minute))
	stage := fixture.stage
	stage.Gates = []string{"network-idle"}
	stage.ResourceVersion = "2"
	stage.LastObservedAt = at.Add(3 * time.Minute)
	stage.FirstObservedAt = stage.LastObservedAt
	snapshot := promotionTestSnapshot(uuid.New(), stage.Integration, stage.ClusterName, stage.LastObservedAt)
	if err := db.ReplacePromotionStageSnapshot(ctx, snapshot, []promotion.Stage{stage}); err != nil {
		t.Fatal(err)
	}
	waiting := accept("historical-gated", at.Add(4*time.Minute))

	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	adapter := stdlib.OpenDBFromPool(db.pool)
	defer func() { _ = adapter.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, adapter, migrations, goose.WithTableName(migrationTable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 75); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	process := uuid.New()
	scope := gate.Scope{Kind: gate.StageScope, ID: stage.ID}
	state, err := db.ChangeServerGateLock(ctx, scope, fixture.actor, &gate.LockRequest{Reason: "hold"})
	if err != nil {
		t.Fatal(err)
	}
	request := gate.OverrideRequest{Subject: gate.PromotionSubject, SubjectID: ungated, Key: gate.LockKey(state.ID),
		Version: "1", Actor: fixture.actor, Reason: "one release", RecordedAt: time.Now().UTC(), EvaluatorProcessID: process}
	if _, err := db.RecordGateOverride(ctx, request); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("historical approval = %v", err)
	}
	request.SubjectID = waiting
	if _, err := db.RecordGateOverride(ctx, request); err != nil {
		t.Fatalf("historical waiting callback = %v", err)
	}
}

func TestWindowMigrationPreservesLockOverrideIntegration(t *testing.T) {
	at := time.Now().UTC().Add(-10 * time.Minute)
	f := newPromotionIntegrationFixture(t, at)
	ctx, db := f.ctx, f.database
	p := createDispatchablePromotion(t, f, at.Add(2*time.Minute))
	scope, err := db.ChangeServerGateLock(ctx, gate.Scope{Kind: gate.GlobalScope}, f.actor, &gate.LockRequest{Reason: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	request := gate.OverrideRequest{Subject: gate.PromotionSubject, SubjectID: p.ID, Key: gate.LockKey(scope.ID), Version: "1",
		Actor: f.actor, Reason: "recover service", RecordedAt: time.Now().UTC(), EvaluatorProcessID: uuid.New()}
	original, err := db.RecordGateOverride(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	adapter := stdlib.OpenDBFromPool(db.pool)
	defer func() { _ = adapter.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, adapter, migrations, goose.WithTableName(migrationTable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 76); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListGateOverrides(ctx, request.Subject, p.ID, request.EvaluatorProcessID)
	if err != nil || len(list.Eligible) != 0 || len(list.Decisions) != 1 {
		t.Fatalf("upgraded subject = %#v, %v", list, err)
	}
	got := list.Decisions[0]
	if got.Reference != request.Reference || got.Actor != request.Actor || got.Reason != request.Reason ||
		!got.RecordedAt.Equal(original.Overrides[0].RecordedAt) || got.ObservedEvidence.Payload.(*gate.ScopeLock).Reason != "maintenance" {
		t.Fatalf("upgraded decision = %#v", got)
	}
}
