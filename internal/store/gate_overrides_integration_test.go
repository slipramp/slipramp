package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestRunGateOverrideIntegration(t *testing.T) {
	ctx, _, db := openIntegrationDatabase(t)
	defer db.Close()
	repo, revision, process := uuid.New(), uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	createGateTestRepository(t, db, repo, revision, at)
	record := gateTestRun(t, db, repo, revision, at)
	if _, err := db.CreateRun(ctx, record, nil); err != nil {
		t.Fatal(err)
	}
	read := func() gate.Requirement {
		t.Helper()
		rows, err := db.ListRunGateSummaries(ctx, []uuid.UUID{record.ID}, process, at)
		if err != nil {
			t.Fatal(err)
		}
		return rows[record.ID][0]
	}
	request := gate.OverrideRequest{Subject: gate.RunSubject, SubjectID: record.ID, Reference: read().Reference,
		Actor:  productidentity.ActorSnapshot{Kind: productidentity.KindUser, DisplayName: "Operator", PrincipalID: uuid.New()},
		Reason: "Incident recovery", RecordedAt: at, EvaluatorProcessID: process}
	invalid := request
	invalid.Reason = strings.Repeat("a", 1025)
	if _, err := db.RecordGateOverride(ctx, invalid); !errors.Is(err, gate.ErrOverrideInvalid) {
		t.Fatalf("oversized reason = %v", err)
	}
	// Two identical submissions record one decision. A different actor cannot replace it.
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := db.RecordGateOverride(ctx, request); results <- err }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	competing := request
	competing.Actor.PrincipalID = uuid.New()
	if _, err := db.RecordGateOverride(ctx, competing); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatal(err)
	}
	if got := read(); got.ReleasedAt != nil || len(got.Overrides) != 1 || got.Overrides[0].Actor != request.Actor {
		t.Fatalf("pending override = %#v", got)
	}
	// Editing the condition blocks assignment and rejects a stale operator form.
	refresh := successfulCatalogRefresh(strings.Repeat("c", 40), at.Add(time.Second), uuid.New())
	refresh.Revision.Catalog = persistedCatalog("1.14.4")
	refresh.Revision.Catalog.Gates[0].Spec.Evaluator.(*gate.PrometheusDefinition).Condition.Value = 2
	if err := db.RecordRepositoryRefresh(ctx, repo, refresh); err != nil {
		t.Fatal(err)
	}
	assertNoGateRunAssignment(t, db, process, at.Add(time.Second))
	if _, err := db.RecordGateOverride(ctx, request); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("stale digest = %v", err)
	}
	request.Reference = read().Reference
	request.Reason = ""
	request.RecordedAt = at.Add(2 * time.Second)
	observation := gate.CurrentObservation{RepositoryID: repo, CatalogRevisionID: refresh.Revision.ID, GateName: "network-idle",
		DefinitionDigest: string(request.Version), EvaluatorProcessID: process, EvaluatedAt: at, ValidUntil: at.Add(time.Minute),
		Observation: gate.Observation{State: gate.ObservationUnavailable, Diagnostic: "Source timed out."}}
	if err := db.UpsertGateObservation(ctx, observation); err != nil {
		t.Fatal(err)
	}
	updated, err := db.RecordGateOverride(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Reference != request.Reference || updated.Scope.ID != repo || updated.Origin != gate.RepositoryOrigin ||
		len(updated.Overrides) != 2 || updated.Overrides[1].Reference != updated.Reference ||
		updated.Overrides[0].Version == updated.Version {
		t.Fatalf("updated override projection = %#v", updated)
	}
	// An unrelated catalog refresh preserves the decision across evaluator restarts.
	next := successfulCatalogRefresh(strings.Repeat("d", 40), at.Add(3*time.Second), uuid.New())
	next.Revision.Catalog = refresh.Revision.Catalog
	if err := db.RecordRepositoryRefresh(ctx, repo, next); err != nil {
		t.Fatal(err)
	}
	process = uuid.New()
	rows, err := db.GetRunSchedulingSnapshots(ctx, []uuid.UUID{record.ID}, process, at.Add(4*time.Second), []byte(`{"colo":2}`), []byte(`{}`))
	if err != nil || len(rows) != 1 || !rows[0].GatesReady || rows[0].EligiblePosition == nil {
		t.Fatalf("override queue = %#v, %v", rows, err)
	}
	assigned, err := db.claimQueuedRunAt(ctx, "colo", testSchedulingCapacity(), process, at.Add(4*time.Second))
	if err != nil || assigned.ID != record.ID {
		t.Fatalf("assignment = %v, %v", assigned.ID, err)
	}
	got := read()
	if got.ReleasedAt == nil || got.Evidence != nil || got.EvaluatedAt != nil || len(got.Overrides) != 2 ||
		got.Overrides[1].Reason != "" || got.Overrides[1].ObservedState != gate.RequirementUnavailable ||
		got.Overrides[1].Diagnostic != observation.Observation.Diagnostic {
		t.Fatalf("released evidence = %#v", got)
	}
	if _, err := db.RecordGateOverride(ctx, request); err != nil {
		t.Fatalf("retry after assignment: %v", err)
	}

	other := gateTestRun(t, db, repo, revision, at)
	other.Snapshot.Action.Gates = []string{"network-idle", "missing"}
	if _, err := db.CreateRun(ctx, other, nil); err != nil {
		t.Fatal(err)
	}
	others, err := db.ListRunGateSummaries(ctx, []uuid.UUID{other.ID}, process, at)
	if err != nil || len(others[other.ID][0].Overrides) != 0 {
		t.Fatalf("other Run = %#v, %v", others, err)
	}
	request.SubjectID = other.ID
	request.Key = "repository/missing"
	if _, err := db.RecordGateOverride(ctx, request); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("override without active definition = %v", err)
	}
	request.Key = "repository/unattached"
	if _, err := db.RecordGateOverride(ctx, request); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("override without attached Gate = %v", err)
	}
	request.Key = "repository/network-idle"
	if _, err := db.CancelRun(ctx, other.ID, at.Add(5*time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordGateOverride(ctx, request); !errors.Is(err, gate.ErrOverrideConflict) {
		t.Fatalf("canceled Run = %v", err)
	}
}
