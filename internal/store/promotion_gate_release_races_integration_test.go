package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/promotion"
)

// Wait for the named SQL operation to block on either the barrier or the writer ahead of it.
func waitForGateQuery(t *testing.T, ctx context.Context, db *Database, query string, barrier, writer int32) int32 {
	t.Helper()
	for {
		var pid int32
		err := db.pool.QueryRow(ctx, `SELECT coalesce(max(pid),0) FROM pg_stat_activity
			WHERE starts_with(query, $1) AND pid NOT IN ($2,$3)
			  AND ($2=ANY(pg_blocking_pids(pid)) OR $3=ANY(pg_blocking_pids(pid)))`,
			"-- name: "+query+" ", barrier, writer).Scan(&pid)
		if err != nil {
			t.Fatalf("wait for %s: %v", query, err)
		}
		if pid != 0 {
			return pid
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPromotionReleaseRacesIntegration(t *testing.T) {
	for _, change := range []string{"lock before dispatch", "window replacement before callback", "window expiry during callback", "cancellation before callback"} {
		t.Run(change, func(t *testing.T) {
			at := time.Now().UTC().Add(-time.Minute)
			f := newPromotionIntegrationFixture(t, at.Add(-2*time.Minute))
			db, process := f.database, uuid.New()
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()

			var record promotion.Promotion
			var err error
			owner := gate.Scope{Kind: gate.StageScope, ID: f.stage.ID}
			var window gate.WeeklyWindow
			lockSubject := change == "cancellation before callback"
			dispatch := change == "lock before dispatch"
			if dispatch {
				record = createDispatchablePromotion(t, f, at)
			} else {
				record, err = db.AcceptPromotionRequest(ctx, f.acceptance(t, f.request("race"), at))
				if err != nil {
					t.Fatal(err)
				}
			}
			expire := change == "window expiry during callback"
			if expire || change == "window replacement before callback" {
				window = closedGateTestWindow(time.Now().UTC())
				if expire {
					window = openGateTestWindow()
				}
				state, err := db.ChangeServerGateWindow(ctx, owner, f.actor, &window)
				if err != nil {
					t.Fatal(err)
				}
				if !expire {
					_, err = db.RecordGateOverride(ctx, gate.OverrideRequest{Subject: gate.PromotionSubject, SubjectID: record.ID,
						Key: gate.WindowKey(state.ID), Version: "1", Actor: f.actor, Reason: "one release", RecordedAt: at, EvaluatorProcessID: process})
					if err != nil {
						t.Fatal(err)
					}
				}
			}

			barrier, err := db.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = barrier.Rollback(context.WithoutCancel(ctx)) }()
			if lockSubject {
				_, err = barrier.Exec(ctx, `SELECT id FROM promotions WHERE id=$1 FOR UPDATE`, record.ID)
			} else {
				_, err = barrier.Exec(ctx, `SELECT id FROM server_gate_scopes WHERE kind='global' FOR UPDATE`)
			}
			if err != nil {
				t.Fatal(err)
			}
			holder := int32(barrier.Conn().PgConn().PID())
			writer := holder
			done := make(chan error, 1)
			if !expire {
				go func() {
					var err error
					switch {
					case lockSubject:
						_, _, err = db.CancelPromotion(ctx, record.ID, time.Now().UTC(), f.actor)
					case dispatch:
						_, err = db.ChangeServerGateLock(ctx, owner, f.actor, &gate.LockRequest{Reason: "maintenance"})
					default:
						_, err = db.ChangeServerGateWindow(ctx, owner, f.actor, &window)
					}
					done <- err
				}()
				query := "LockServerGateScopes"
				if lockSubject {
					query = "LockPromotion"
				}
				writer = waitForGateQuery(t, ctx, db, query, holder, holder)
			}

			type result struct {
				status promotion.KargoResponseStatus
				err    error
			}
			release := make(chan result, 1)
			go func() {
				if dispatch {
					_, err := db.ClaimPromotionCreation(ctx, promotion.CreationDispatchClaim{ClusterName: f.stage.ClusterName, GateEvaluatorProcessID: process,
						ClaimedAt: time.Now().UTC(), RetryDelays: []time.Duration{time.Minute}, ExhaustedMessage: "exhausted"})
					release <- result{err: err}
				} else {
					status, err := db.ReleasePromotion(ctx, record.ID, process)
					release <- result{status, err}
				}
			}()
			query := "SharePromotionGateScopes"
			if lockSubject {
				query = "LockPromotion"
			}
			waitForGateQuery(t, ctx, db, query, holder, writer)
			if expire {
				_, err = barrier.Exec(ctx, `UPDATE server_gate_scopes SET window_valid_until=clock_timestamp(),
					window_next_transition=NULL WHERE stage_id=$1`, f.stage.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if !expire {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			got := <-release
			switch {
			case dispatch:
				if !errors.Is(got.err, promotion.ErrNoCreationDispatch) {
					t.Fatalf("claim after lock = %v", got.err)
				}
			case lockSubject:
				if got.err != nil || got.status != promotion.KargoResponseStatusCanceling {
					t.Fatalf("canceled callback = %+v", got)
				}
			default:
				if got.err != nil || got.status != promotion.KargoResponseStatusGated {
					t.Fatalf("closed window callback = %+v", got)
				}
			}
			stored, err := db.queries.GetPromotion(ctx, databaseUUID(record.ID))
			if err != nil || stored.CallbackReleased || stored.DispatchAttemptCount != 0 || stored.ServerGateReleaseEvidence != nil {
				t.Fatalf("partial release after %s: %v", change, err)
			}
		})
	}
}
