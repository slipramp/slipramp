package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gate"
)

func TestWeeklyGateSchedulePersistenceIntegration(t *testing.T) {
	f := newPromotionIntegrationFixture(t, time.Now().UTC())
	ctx, db := f.ctx, f.database
	scope := gate.Scope{Kind: gate.StageScope, ID: f.stage.ID}
	days := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	w := gate.WeeklyWindow{TimeZone: "UTC", Weekly: []gate.WeeklyInterval{{Days: days, Start: "00:00", End: "24:00"}}}
	if _, err := db.ChangeServerGateLock(ctx, scope, f.actor, &gate.LockRequest{Reason: "maintenance"}); err != nil {
		t.Fatal(err)
	}
	set, err := db.ChangeServerGateWindow(ctx, scope, f.actor, &w)
	if err != nil || set.WindowVersion != 1 || set.LockVersion != 1 || set.Window == nil || set.Window.State != gate.RequirementPassing ||
		len(set.WindowEvents) != 1 || set.WindowEvents[0].Actor != f.actor || len(set.Events) != 1 {
		t.Fatalf("set = %#v, %v", set, err)
	}
	replaced, err := db.ChangeServerGateWindow(ctx, scope, f.actor, &w)
	if err != nil || replaced.WindowVersion != 2 || replaced.Window.Digest != set.Window.Digest {
		t.Fatalf("replace = %#v, %v", replaced, err)
	}

	// A suspended coordinator leaves an expired pass in storage.
	_, err = db.pool.Exec(ctx, `UPDATE server_gate_scopes SET window_evaluated_at=now()-interval '2 hours',
		window_valid_until=now()-interval '1 hour' WHERE id=$1`, databaseUUID(set.ID))
	if err != nil {
		t.Fatal(err)
	}
	stale, err := db.GetServerGateScope(ctx, scope)
	if err != nil || stale.Window.State != gate.RequirementUnavailable || stale.Window.Observation.State != gate.RequirementPassing {
		t.Fatalf("stale pass = %#v, %v", stale.Window, err)
	}
	if _, err := db.RefreshServerWindows(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.GetServerGateScope(ctx, scope)
	if err != nil || fresh.Window.State != gate.RequirementPassing || fresh.WindowVersion != 2 || len(fresh.WindowEvents) != 2 {
		t.Fatalf("refreshed = %#v, %v", fresh, err)
	}
	restarted, err := db.WindowCheckTime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RefreshServerWindows(ctx, restarted); err != nil {
		t.Fatal(err)
	}
	fresh, err = db.GetServerGateScope(ctx, scope)
	if err != nil || fresh.Window.Observation.EvaluatedAt.Before(restarted) {
		t.Fatalf("startup refresh = %#v, %v", fresh, err)
	}
	delay, err := db.RefreshServerWindows(ctx, restarted)
	if err != nil || delay <= 0 {
		t.Fatalf("next deadline = %v, %v", delay, err)
	}
	for range 2 {
		cleared, err := db.ChangeServerGateWindow(ctx, scope, f.actor, nil)
		if err != nil || cleared.Window != nil || cleared.WindowVersion != 3 || cleared.LockVersion != 1 || len(cleared.WindowEvents) != 3 ||
			cleared.WindowEvents[0].Action != "remove" || cleared.WindowEvents[1].Definition == nil {
			t.Fatalf("remove = %#v, %v", cleared, err)
		}
	}
}

func TestWindowOverridesIntegration(t *testing.T) {
	at := time.Now().UTC().Add(-10 * time.Minute)
	f := newPromotionIntegrationFixture(t, at)
	ctx, db := f.ctx, f.database
	p := createDispatchablePromotion(t, f, at.Add(2*time.Minute))
	base, err := db.GetRun(ctx, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	run := gateTestRun(t, db, f.repositoryID, base.CatalogRevisionID, at.Add(time.Minute))
	run.Snapshot.Action.Intent = catalog.ActionIntentOperation
	if _, err := db.CreateRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	owner := gate.Scope{Kind: gate.GlobalScope}
	tomorrow := strings.ToLower(time.Now().UTC().AddDate(0, 0, 2).Weekday().String())
	w := gate.WeeklyWindow{TimeZone: "UTC", Weekly: []gate.WeeklyInterval{{Days: []string{tomorrow}, Start: "09:00", End: "17:00"}}}
	scope, err := db.ChangeServerGateWindow(ctx, owner, f.actor, &w)
	if err != nil {
		t.Fatal(err)
	}
	key := gate.WindowKey(scope.ID)
	process := uuid.New()
	request := gate.OverrideRequest{Subject: gate.RunSubject, SubjectID: run.ID, Key: key, Version: "1", Actor: f.actor,
		Reason: "one release", RecordedAt: time.Now().UTC(), EvaluatorProcessID: process}
	for range 2 {
		got, err := db.RecordGateOverride(ctx, request)
		if err != nil || got.State != gate.RequirementBlocking || len(got.Overrides) != 1 {
			t.Fatalf("Run override = %#v, %v", got, err)
		}
	}
	promo := request
	promo.Subject, promo.SubjectID = gate.PromotionSubject, p.ID
	list, err := db.ListGateOverrides(ctx, promo.Subject, p.ID, process)
	if err != nil || len(list.Eligible) != 1 || list.Eligible[0].Key != key || len(list.Decisions) != 0 {
		t.Fatalf("Promotion = %#v, %v", list, err)
	}
	if _, err := db.RecordGateOverride(ctx, promo); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ChangeServerGateWindow(ctx, owner, f.actor, &w); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordGateOverride(ctx, promo); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatal(err)
	}
	list, err = db.ListGateOverrides(ctx, promo.Subject, p.ID, process)
	if err != nil || len(list.Eligible) != 1 || list.Eligible[0].Version != "2" || len(list.Decisions) != 1 {
		t.Fatalf("replacement = %#v, %v", list, err)
	}
	evidence, ok := list.Decisions[0].ObservedEvidence.Payload.(*gate.WindowEvidence)
	if !ok || evidence.Digest != scope.Window.Digest || evidence.TimeZone != "UTC" || evidence.Observation.NextTransition == nil ||
		!evidence.Observation.EvaluatedAt.Equal(scope.Window.Observation.EvaluatedAt) {
		t.Fatalf("saved evidence = %#v", evidence)
	}

	w.Weekly[0] = gate.WeeklyInterval{Days: []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}, Start: "00:00", End: "24:00"}
	if _, err := db.ChangeServerGateWindow(ctx, owner, f.actor, &w); err != nil {
		t.Fatal(err)
	}
	promo.Version = "3"
	if _, err := db.RecordGateOverride(ctx, promo); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("passing = %v", err)
	}
	_, err = db.pool.Exec(ctx, `UPDATE server_gate_scopes SET window_evaluated_at=now()-interval '2 minutes',
		window_valid_until=now()-interval '1 minute' WHERE id=$1`, databaseUUID(scope.ID))
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.RecordGateOverride(ctx, promo)
	if err != nil || got.State != gate.RequirementUnavailable || got.Diagnostic == "" {
		t.Fatalf("stale = %#v, %v", got, err)
	}
	if _, err := db.ChangeServerGateWindow(ctx, owner, f.actor, nil); err != nil {
		t.Fatal(err)
	}
	list, err = db.ListGateOverrides(ctx, promo.Subject, p.ID, process)
	if err != nil || len(list.Eligible) != 0 || len(list.Decisions) != 2 || list.Decisions[1].ObservedState != gate.RequirementUnavailable ||
		list.Decisions[1].Diagnostic != got.Diagnostic {
		t.Fatalf("removed = %#v, %v", list, err)
	}
	if _, err := db.RecordGateOverride(ctx, promo); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("removed override = %v", err)
	}
}
