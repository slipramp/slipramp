package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func createServerGatedRun(t *testing.T, f promotionIntegrationFixture, intent catalog.ActionIntent,
	at time.Time, names ...string) run.Run {
	t.Helper()
	base, err := f.database.GetRun(f.ctx, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	record := gateTestRun(t, f.database, f.repositoryID, base.CatalogRevisionID, at)
	record.Snapshot.Action.Intent = intent
	record.Snapshot.Action.Gates = names
	created, err := f.database.CreateRun(f.ctx, record, nil)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func (d *Database) claimQueuedRunAt(ctx context.Context, cluster string, capacity run.SchedulingCapacity, process uuid.UUID, at time.Time) (run.Run, error) {
	return d.claimQueuedRun(ctx, cluster, capacity, process, fixedGateClock(at))
}

func TestRunAssignmentComposesGatePoliciesIntegration(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPromotionIntegrationFixture(t, at.Add(-time.Hour))
	ctx, db, process := f.ctx, f.database, uuid.New()
	if _, err := db.CancelRun(ctx, f.runID, at, &f.actor); err != nil {
		t.Fatal(err)
	}
	op := createServerGatedRun(t, f, catalog.ActionIntentOperation, at, "network-idle")
	reconcile := createServerGatedRun(t, f, catalog.ActionIntentReconcile, at.Add(time.Second))
	gatedPreview := createServerGatedRun(t, f, catalog.ActionIntentPreview, at.Add(2*time.Second), "network-idle")
	preview := createServerGatedRun(t, f, catalog.ActionIntentPreview, at.Add(3*time.Second))
	closed := closedGateTestWindow(time.Now().UTC())
	owners := []gate.Scope{{Kind: gate.GlobalScope}, {Kind: gate.TargetScope, ID: op.TargetID}}
	setGateReleasePolicies(t, f, closed, owners...)
	stage := gate.Scope{Kind: gate.StageScope, ID: f.stage.ID}
	if _, err := db.ChangeServerGateLock(ctx, stage, f.actor, &gate.LockRequest{Reason: "Stage only"}); err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{op.ID, reconcile.ID, gatedPreview.ID, preview.ID}
	now := time.Now().UTC()
	queue, err := db.GetRunSchedulingSnapshots(ctx, ids, process, now, []byte(`{"colo":4}`), []byte(`{}`))
	if err != nil || len(queue) != len(ids) {
		t.Fatalf("queue = %#v, %v", queue, err)
	}
	for _, item := range queue {
		if item.RunID == preview.ID {
			if !item.GatesReady || item.EligiblePosition == nil || *item.EligiblePosition != 1 {
				t.Fatalf("preview position = %#v", item)
			}
		} else if item.GatesReady || item.EligiblePosition != nil {
			t.Fatalf("blocked Run has a queue position: %#v", item)
		}
	}
	service := run.NewService(run.Dependencies{Gates: db, GateEvaluatorProcessID: process})
	summaries, err := service.GateSummaries(ctx, []run.Run{op, reconcile, gatedPreview, preview}, now)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"global-lock", "target-lock", "global-window", "target-window", "network-idle"}
	if len(summaries[op.ID]) != len(names) || len(summaries[reconcile.ID]) != 4 ||
		len(summaries[gatedPreview.ID]) != 1 || summaries[preview.ID].State() != gate.SummaryNotRequired {
		t.Fatalf("queued summaries = %#v", summaries)
	}
	for i, check := range summaries[op.ID] {
		if check.Name != names[i] || (i < 4 && (check.State != gate.RequirementBlocking || check.Origin != gate.ServerOrigin || check.Version != "1")) {
			t.Fatalf("queued check %d = %#v", i, check)
		}
	}
	metrics, err := db.MetricsSnapshotSource(process).LoadMetricsSnapshot(ctx, now)
	if err != nil || metrics.OldestEligibleRunCreatedAt == nil || !metrics.OldestEligibleRunCreatedAt.Equal(preview.CreatedAt) ||
		metrics.WaitingGateSubjects["run"] != 3 {
		t.Fatalf("queue metrics = %#v, %v", metrics, err)
	}
	claim := func() (run.Run, error) { return db.ClaimQueuedRun(ctx, "colo", testSchedulingCapacity(), process) }
	first, err := claim()
	if err != nil || first.ID != preview.ID {
		t.Fatalf("first eligible Run = %s, %v", first.ID, err)
	}
	finishAssignedRun(t, db, first, time.Now().UTC())
	overrideReleaseGates(t, f, gate.RunSubject, op.ID, process, len(names), func() {
		if _, err := claim(); !errors.Is(err, run.ErrNoRunAssignment) {
			t.Fatalf("assignment before all overrides: %v", err)
		}
	})
	assigned, err := claim()
	if err != nil || assigned.ID != op.ID || assigned.AssignedAt == nil {
		t.Fatalf("assignment = %#v, %v", assigned, err)
	}
	readRelease := func() []byte {
		t.Helper()
		var data []byte
		var repoAt time.Time
		err := db.pool.QueryRow(ctx, `SELECT r.server_gate_release_evidence, g.released_at
			FROM runs r JOIN run_gates g ON g.run_id=r.id WHERE r.id=$1`, op.ID).Scan(&data, &repoAt)
		if err != nil || !repoAt.Equal(*assigned.AssignedAt) {
			t.Fatalf("release timestamps = %v, %v", repoAt, err)
		}
		return data
	}
	before := readRelease()
	requireGateReleaseEvidence(t, before, *assigned.AssignedAt, f.actor, owners)
	for _, owner := range owners {
		if _, err := db.ChangeServerGateLock(ctx, owner, f.actor, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ChangeServerGateWindow(ctx, owner, f.actor, nil); err != nil {
			t.Fatal(err)
		}
	}
	summaries, err = service.GateSummaries(ctx, []run.Run{assigned, reconcile}, time.Now().UTC())
	checks := summaries[assigned.ID]
	if err != nil || checks.State() != gate.SummaryReleased || len(checks) != len(names) || summaries[reconcile.ID].State() != gate.SummaryNotRequired {
		t.Fatalf("summaries after policy removal = %#v, %v", summaries, err)
	}
	for i, check := range checks {
		if check.Name != names[i] || check.ReleasedAt == nil || !check.ReleasedAt.Equal(*assigned.AssignedAt) ||
			len(check.Overrides) != 1 || check.Overrides[0].Reference != check.Reference {
			t.Fatalf("released check %d = %#v", i, check)
		}
	}
	again, err := db.GetAssignedRunForCluster(ctx, "colo")
	if err != nil || again.ID != op.ID || !again.AssignedAt.Equal(*assigned.AssignedAt) || !bytes.Equal(before, readRelease()) {
		t.Fatalf("redelivered Run = %#v, %v", again, err)
	}
}

func TestRunRechecksScopeAtAssignmentIntegration(t *testing.T) {
	for _, change := range []string{"lock", "unlock", "expire window"} {
		t.Run(change, func(t *testing.T) {
			at := time.Now().UTC().Add(-time.Minute)
			f := newPromotionIntegrationFixture(t, at.Add(-time.Hour))
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			db := f.database
			if _, err := db.CancelRun(ctx, f.runID, at, &f.actor); err != nil {
				t.Fatal(err)
			}
			record := createServerGatedRun(t, f, catalog.ActionIntentReconcile, at)
			owner := gate.Scope{Kind: gate.TargetScope, ID: record.TargetID}
			if change == "unlock" {
				if _, err := db.ChangeServerGateLock(ctx, owner, f.actor, &gate.LockRequest{Reason: "maintenance"}); err != nil {
					t.Fatal(err)
				}
			}
			if change == "expire window" {
				always := openGateTestWindow()
				if _, err := db.ChangeServerGateWindow(ctx, owner, f.actor, &always); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := db.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
			q := db.queries.WithTx(tx)
			rows, err := q.LockServerGateScopes(ctx, storedb.LockServerGateScopesParams{Kind: "target", OwnerID: databaseUUID(owner.ID)})
			if err != nil || len(rows) != 2 {
				t.Fatalf("lock scopes = %v, %v", rows, err)
			}
			claim := func() (run.Run, error) {
				return db.ClaimQueuedRun(ctx, record.ClusterName, testSchedulingCapacity(), uuid.Nil)
			}
			if change == "unlock" {
				if _, err := changeScopeLock(ctx, q, rows[1], f.actor, nil, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
				if _, err := claim(); !errors.Is(err, run.ErrNoRunAssignment) {
					t.Fatalf("claim before unlock committed = %v", err)
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				got, err := claim()
				if err != nil || got.ID != record.ID {
					t.Fatalf("claim after unlock = %s, %v", got.ID, err)
				}
				return
			}
			done := make(chan error, 1)
			go func() { _, err := claim(); done <- err }()
			for {
				var blocked bool
				err := db.pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,
					int32(tx.Conn().PgConn().PID())).Scan(&blocked)
				if err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("claim finished while writer held scope: %v", err)
				case <-time.After(10 * time.Millisecond):
				}
			}
			if change == "lock" {
				_, err = changeScopeLock(ctx, q, rows[1], f.actor, &gate.LockRequest{Reason: "maintenance"}, time.Now().UTC())
			} else {
				// Expire the passing observation between candidate selection and the final clock sample.
				_, err = tx.Exec(ctx, `UPDATE server_gate_scopes SET window_valid_until=clock_timestamp(), window_next_transition=NULL WHERE id=$1`, rows[1].ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, run.ErrNoRunAssignment) {
				t.Fatalf("claim after %s = %v", change, err)
			}
			stored, err := db.queries.GetRun(ctx, databaseUUID(record.ID))
			if err != nil || stored.Phase != "queued" || stored.AssignedAt.Valid || stored.ServerGateReleaseEvidence != nil {
				t.Fatalf("partial release after %s", change)
			}
			if change == "expire window" {
				summary := func() gate.Requirement {
					t.Helper()
					items, err := db.ListRunGateSummaries(ctx, []uuid.UUID{record.ID}, uuid.Nil, time.Now().UTC())
					if err != nil || len(items[record.ID]) != 1 {
						t.Fatalf("window summary = %#v, %v", items, err)
					}
					return items[record.ID][0]
				}
				if summary().State != gate.RequirementUnavailable {
					t.Fatal("expired window still passing")
				}
				if _, err := db.RefreshServerWindows(ctx, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
				got, err := claim()
				if err != nil || got.ID != record.ID {
					t.Fatalf("claim after refresh = %#v, %v", got, err)
				}
				if _, err := db.ChangeServerGateLock(ctx, gate.Scope{Kind: gate.GlobalScope}, f.actor, &gate.LockRequest{Reason: "hold later Runs"}); err != nil {
					t.Fatal(err)
				}
				check := summary()
				if check.State != gate.RequirementReleased || len(check.Overrides) != 0 ||
					check.Evidence.Payload.(*gate.WindowEvidence).Observation.State != gate.RequirementPassing {
					t.Fatalf("released window = %#v", check)
				}
			}
		})
	}
}

func TestRunClaimRacingWindowReplacementRejectsPriorWaiverIntegration(t *testing.T) {
	at := time.Now().UTC().Add(-time.Minute)
	f := newPromotionIntegrationFixture(t, at.Add(-time.Hour))
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	db, process := f.database, uuid.New()
	if _, err := db.CancelRun(ctx, f.runID, at, &f.actor); err != nil {
		t.Fatal(err)
	}
	record := createServerGatedRun(t, f, catalog.ActionIntentOperation, at)
	owner := gate.Scope{Kind: gate.TargetScope, ID: record.TargetID}
	closed := closedGateTestWindow(time.Now().UTC())
	scope, err := db.ChangeServerGateWindow(ctx, owner, f.actor, &closed)
	if err != nil {
		t.Fatal(err)
	}
	request := gate.OverrideRequest{Subject: gate.RunSubject, SubjectID: record.ID, Key: gate.WindowKey(scope.ID),
		Version: "1", Actor: f.actor, Reason: "release outside hours", RecordedAt: at, EvaluatorProcessID: process}
	if _, err := db.RecordGateOverride(ctx, request); err != nil {
		t.Fatal(err)
	}

	// Queue the policy writer first, then let the claim select the Run using its version-one waiver.
	barrier, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = barrier.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := barrier.Exec(ctx, `SELECT id FROM server_gate_scopes WHERE kind = 'global' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	holder := int32(barrier.Conn().PgConn().PID())

	write := make(chan error, 1)
	go func() { _, err := db.ChangeServerGateWindow(ctx, owner, f.actor, &closed); write <- err }()
	writer := waitForGateQuery(t, ctx, db, "LockServerGateScopes", holder, holder)
	claim := make(chan error, 1)
	go func() {
		_, err := db.ClaimQueuedRun(ctx, record.ClusterName, testSchedulingCapacity(), process)
		claim <- err
	}()
	waitForGateQuery(t, ctx, db, "ShareRunGateScopes", holder, writer)
	if err := barrier.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-write; err != nil {
		t.Fatal(err)
	}
	if err := <-claim; !errors.Is(err, run.ErrNoRunAssignment) {
		t.Fatalf("claim with prior waiver = %v", err)
	}
	collection, err := db.ListGateOverrides(ctx, gate.RunSubject, record.ID, process)
	if err != nil || len(collection.Eligible) != 1 || collection.Eligible[0].Version != "2" ||
		len(collection.Decisions) != 1 || collection.Decisions[0].Version != "1" {
		t.Fatalf("after replacement = %#v, %v", collection, err)
	}
}
