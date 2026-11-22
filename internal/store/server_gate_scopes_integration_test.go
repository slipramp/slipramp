package store

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func TestServerGateLockChangesIntegration(t *testing.T) {
	fixture := newPromotionIntegrationFixture(t, time.Now().UTC())
	ctx, db := fixture.ctx, fixture.database
	global := gate.Scope{Kind: gate.GlobalScope}
	stage := gate.Scope{Kind: gate.StageScope, ID: fixture.stage.ID}
	target := gate.Scope{Kind: gate.TargetScope, ID: activeTargetID(t, db, fixture.repositoryID, "network")}

	// Of two different requests at one unlocked scope, exactly one creates a hold.
	start := make(chan struct{})
	results := make(chan error, 2)
	expiry := time.Now().UTC().Add(time.Hour)
	for _, reason := range []string{"database upgrade", "network cutover"} {
		go func() {
			<-start
			_, err := db.ChangeServerGateLock(ctx, global, fixture.actor, &gate.LockRequest{Reason: reason, ExpiresAt: &expiry})
			results <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first != nil {
		first, second = second, first
	}
	if first != nil || !errors.Is(second, gate.ErrLockConflict) {
		t.Fatalf("concurrent lock results = %v, %v", first, second)
	}
	locked, err := db.GetServerGateScope(ctx, global)
	if err != nil || locked.LockVersion != 1 || locked.Lock == nil || locked.Lock.Actor != fixture.actor ||
		len(locked.Events) != 1 || locked.Events[0].Action != "lock" || locked.Events[0].Reason != locked.Lock.Reason ||
		!locked.Events[0].RecordedAt.Equal(locked.Lock.CreatedAt) || locked.Events[0].ExpiresAt == nil ||
		!locked.Events[0].ExpiresAt.Equal(expiry.Truncate(time.Microsecond)) {
		t.Fatalf("global lock = %#v, %v", locked, err)
	}
	retry := &gate.LockRequest{Reason: locked.Lock.Reason, ExpiresAt: &expiry}
	replayed, err := db.ChangeServerGateLock(ctx, global, fixture.actor, retry)
	if err != nil || replayed.LockVersion != 1 || len(replayed.Events) != 1 {
		t.Fatalf("retry = %#v, %v", replayed, err)
	}
	other := fixture.principal
	other.ID = uuid.New()
	other.DisplayName = "On-call operator"
	if _, err := db.CreatePrincipal(ctx, other); err != nil {
		t.Fatal(err)
	}
	otherActor, err := other.ActorSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ChangeServerGateLock(ctx, global, otherActor, retry); !errors.Is(err, gate.ErrLockConflict) {
		t.Fatalf("different actor retry = %v", err)
	}

	local, err := db.ChangeServerGateLock(ctx, target, otherActor, &gate.LockRequest{Reason: "Target maintenance"})
	if err != nil || local.ID == locked.ID || local.Lock == nil || local.Lock.ExpiresAt != nil {
		t.Fatalf("local lock = %#v, %v", local, err)
	}
	unlocked, err := db.GetServerGateScope(ctx, stage)
	if err != nil || unlocked.LockVersion != 0 || unlocked.Lock != nil {
		t.Fatalf("independent Stage = %#v, %v", unlocked, err)
	}
	for range 2 {
		cleared, err := db.ChangeServerGateLock(ctx, global, otherActor, nil)
		if err != nil || cleared.LockVersion != 2 || cleared.Lock != nil || len(cleared.Events) != 2 ||
			cleared.Events[0].Actor != otherActor || cleared.Events[0].Action != "unlock" ||
			cleared.Events[0].Reason != locked.Lock.Reason {
			t.Fatalf("unlock = %#v, %v", cleared, err)
		}
	}
	again, err := db.ChangeServerGateLock(ctx, global, fixture.actor, retry)
	if err != nil || again.LockVersion != 3 || len(again.Events) != 3 || again.ID != locked.ID {
		t.Fatalf("re-lock = %#v, %v", again, err)
	}
	if _, err := db.UnlockServerGateVersion(ctx, global, otherActor, locked.LockVersion); !errors.Is(err, gate.ErrLockVersion) {
		t.Fatalf("stale unlock = %v", err)
	}
	current, err := db.GetServerGateScope(ctx, global)
	if err != nil || current.Lock == nil || current.LockVersion != again.LockVersion || len(current.Events) != len(again.Events) {
		t.Fatalf("scope after stale unlock = %#v, %v", current, err)
	}
	for range 2 {
		cleared, err := db.UnlockServerGateVersion(ctx, global, otherActor, again.LockVersion)
		if err != nil || cleared.Lock != nil || cleared.LockVersion != again.LockVersion+1 || len(cleared.Events) != len(again.Events)+1 {
			t.Fatalf("versioned unlock = %#v, %v", cleared, err)
		}
	}
	local, err = db.GetServerGateScope(ctx, target)
	if err != nil || local.Lock == nil || local.LockVersion != 1 {
		t.Fatalf("Target after global changes = %#v, %v", local, err)
	}
	missing := gate.Scope{Kind: gate.StageScope, ID: target.ID}
	if _, err := db.ChangeServerGateLock(ctx, missing, fixture.actor, nil); !errors.Is(err, gate.ErrScopeNotFound) {
		t.Fatalf("missing Stage = %v", err)
	}
}

func TestServerGateLockExpiryIntegration(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Hour)
	fixture := newPromotionIntegrationFixture(t, at)
	ctx, db := fixture.ctx, fixture.database
	owner := gate.Scope{Kind: gate.StageScope, ID: fixture.stage.ID}
	// Record a historical lock so both sides of its exclusive expiry are deterministic.
	created := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	expires := created.Add(time.Minute)
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.queries.WithTx(tx)
	rows, err := q.LockServerGateScopes(ctx, storedb.LockServerGateScopesParams{
		Kind: string(owner.Kind), OwnerID: databaseUUID(owner.ID),
	})
	if err != nil || len(rows) != 2 {
		t.Fatalf("lock scopes = %v, %v", rows, err)
	}
	request := &gate.LockRequest{Reason: "old hold", ExpiresAt: &expires}
	row, err := changeScopeLock(ctx, q, rows[1], fixture.actor, request, created)
	if err != nil {
		t.Fatal(err)
	}
	if !serverGateLockActive(row, expires.Add(-time.Microsecond)) || serverGateLockActive(row, expires) {
		t.Fatal("incorrect expiry boundary")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := db.GetServerGateScope(ctx, owner)
	if err != nil || state.Lock != nil || state.LockVersion != 1 || len(state.Events) != 1 ||
		state.Events[0].ExpiresAt == nil || !state.Events[0].ExpiresAt.Equal(expires) {
		t.Fatalf("expired lock = %#v, %v", state, err)
	}
	// An override submitted during the hold but processed after expiry is stale.
	record := createDispatchablePromotion(t, fixture, at.Add(2*time.Minute))
	_, err = db.RecordGateOverride(ctx, gate.OverrideRequest{
		Subject: gate.PromotionSubject, SubjectID: record.ID, Key: gate.LockKey(state.ID), Version: "1",
		Actor: fixture.actor, Reason: "one release", RecordedAt: created.Add(time.Second), EvaluatorProcessID: uuid.New(),
	})
	if !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("override after expiry = %v", err)
	}
	_, err = db.ChangeServerGateLock(ctx, owner, fixture.actor, request)
	if !errors.Is(err, gate.ErrLockExpiry) {
		t.Fatalf("past expiry = %v", err)
	}
	state, err = db.ChangeServerGateLock(ctx, owner, fixture.actor, nil)
	if err != nil || state.LockVersion != 1 || len(state.Events) != 1 {
		t.Fatalf("clear expired lock = %#v, %v", state, err)
	}
	state, err = db.ChangeServerGateLock(ctx, owner, fixture.actor, &gate.LockRequest{Reason: "new hold"})
	if err != nil || state.Lock == nil || state.LockVersion != 2 || len(state.Events) != 2 {
		t.Fatalf("replace expired lock = %#v, %v", state, err)
	}
}

func TestServerGateScopeBackfillAndReactivationIntegration(t *testing.T) {
	at := time.Now().UTC()
	fixture := newPromotionIntegrationFixture(t, at)
	ctx, db := fixture.ctx, fixture.database
	targetID := activeTargetID(t, db, fixture.repositoryID, "network")
	owners := []gate.Scope{
		{Kind: gate.GlobalScope},
		{Kind: gate.TargetScope, ID: targetID},
		{Kind: gate.StageScope, ID: fixture.stage.ID},
	}
	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	adapter := stdlib.OpenDBFromPool(db.pool)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Error(err)
		}
	}()
	provider, err := goose.NewProvider(goose.DialectPostgres, adapter, migrations, goose.WithTableName(migrationTable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 74); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	ids := make(map[gate.Scope]uuid.UUID)
	for _, owner := range owners {
		state, err := db.ChangeServerGateLock(ctx, owner, fixture.actor, &gate.LockRequest{Reason: "hold through archival"})
		if err != nil || state.ID == uuid.Nil {
			t.Fatalf("backfilled %s scope = %#v, %v", owner.Kind, state, err)
		}
		ids[owner] = state.ID
	}
	for i, commit := range []string{"d", "e"} {
		observed := at.Add(time.Duration(2+i) * time.Minute)
		refresh := successfulCatalogRefresh(strings.Repeat(commit, 40), observed, uuid.New())
		refresh.Revision.Catalog = persistedCatalog("1.14.3")
		stage := fixture.stage
		stage.ID = uuid.New() // Re-observation resolves the durable Stage identity.
		stage.FirstObservedAt = observed
		stage.LastObservedAt = observed
		stages := []promotion.Stage{stage}
		if i == 0 {
			refresh.Revision.Catalog.Targets = nil
			stages = nil
		}
		if err := db.RecordRepositoryRefresh(ctx, fixture.repositoryID, refresh); err != nil {
			t.Fatal(err)
		}
		snapshot := promotionTestSnapshot(uuid.New(), stage.Integration, stage.ClusterName, observed)
		if err := db.ReplacePromotionStageSnapshot(ctx, snapshot, stages); err != nil {
			t.Fatal(err)
		}
		wantArchived := i == 0
		target, err := db.GetTarget(ctx, targetID)
		if err != nil || (target.ArchivedAt != nil) != wantArchived {
			t.Fatalf("archived Target after catalog %s = %v, %v", commit, target.ArchivedAt, err)
		}
		persistedStage, err := db.GetPromotionStageByIdentity(ctx, stage.Integration, stage.Project, stage.Name)
		if err != nil || persistedStage.ID != fixture.stage.ID || (persistedStage.ArchivedAt != nil) != wantArchived {
			t.Fatalf("Stage after snapshot at %v = %#v, %v", observed, persistedStage, err)
		}
		for _, owner := range owners {
			state, err := db.GetServerGateScope(ctx, owner)
			if err != nil || state.ID != ids[owner] || state.Lock == nil || state.LockVersion != 1 || len(state.Events) != 1 {
				t.Fatalf("scope after catalog %s = %#v, %v", commit, state, err)
			}
		}
	}
}
