package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

func newWebhookGateFixture(t *testing.T, now time.Time) (promotionIntegrationFixture, uuid.UUID) {
	t.Helper()
	f := newPromotionIntegrationFixture(t, now.Add(-3*time.Minute))
	if _, err := f.database.CancelRun(f.ctx, f.runID, now, &f.actor); err != nil {
		t.Fatal(err)
	}
	revision := activateWebhookGate(t, f, now, strings.Repeat("d", 40), "deploy")
	stage := &f.stage
	stage.ResourceVersion = "2"
	stage.Gates = []string{"network-idle"}
	stage.FirstObservedAt = now
	stage.LastObservedAt = now
	snapshot := promotionTestSnapshot(uuid.New(), stage.Integration, stage.ClusterName, now)
	if err := f.database.ReplacePromotionStageSnapshot(f.ctx, snapshot, []promotion.Stage{*stage}); err != nil {
		t.Fatal(err)
	}
	return f, revision
}

func activateWebhookGate(t *testing.T, f promotionIntegrationFixture, now time.Time, commit, policy string) uuid.UUID {
	t.Helper()
	refresh := successfulCatalogRefresh(commit, now, uuid.New())
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	refresh.Revision.Catalog.Gates[0].Spec = gate.Definition{Evaluator: &gate.WebhookDefinition{
		Source: "policy", Input: map[string]any{"policy": policy},
	}}
	if err := f.database.RecordRepositoryRefresh(f.ctx, f.repositoryID, refresh); err != nil {
		t.Fatal(err)
	}
	return refresh.Revision.ID
}

func createWebhookRun(t *testing.T, f promotionIntegrationFixture, revision uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()
	r := gateTestRun(t, f.database, f.repositoryID, revision, at)
	r.CommitSHA = strings.Repeat("d", 40)
	if _, err := f.database.CreateRun(f.ctx, r, nil); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func createWebhookPromotion(t *testing.T, f promotionIntegrationFixture, name string, at time.Time) uuid.UUID {
	t.Helper()
	p, err := f.database.AcceptPromotionRequest(f.ctx, f.acceptance(t, f.request(name), at))
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func startWebhookCoordinator(
	t *testing.T, f promotionIntegrationFixture, url string, processID uuid.UUID, interval time.Duration,
) (<-chan gate.CurrentObservation, func()) {
	t.Helper()
	observer := recordingGateStore{Database: f.database, observations: make(chan gate.CurrentObservation)}
	source := gate.WebhookSource{
		URL: url, EvaluationInterval: interval, Timeout: time.Second, MaximumConcurrency: 2,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coordinator, err := gate.NewEvaluationCoordinator(observer, gate.SourceSet{"policy": source}, processID, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		coordinator.Run(ctx)
	}()
	return observer.observations, func() {
		cancel()
		<-done
	}
}

func TestWaitingWebhookSubjectsRecoverAfterRestartIntegration(t *testing.T) {
	now := time.Now().UTC()
	fixture, revision := newWebhookGateFixture(t, now)
	db, ctx := fixture.database, fixture.ctx
	runID := createWebhookRun(t, fixture, revision, now)
	promotionID := createWebhookPromotion(t, fixture, "restart", now)

	var available atomic.Bool
	available.Store(true)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !available.Load() {
			http.Error(w, "private upstream response", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"apiVersion":"slipramp.com/gate-evaluation/v1alpha1","decision":"passing","code":"ready","message":"Ready"}`)
	}))
	defer endpoint.Close()

	collect := func(updates <-chan gate.CurrentObservation, want gate.ObservationState) []gate.CurrentObservation {
		t.Helper()
		pending := map[uuid.UUID]bool{runID: true, promotionID: true}
		var records []gate.CurrentObservation
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for len(pending) > 0 {
			select {
			case record := <-updates:
				if pending[record.SubjectID] && record.Observation.State == want {
					records = append(records, record)
					delete(pending, record.SubjectID)
				}
			case <-deadline.C:
				t.Fatalf("timed out waiting for %s observations for %v", want, pending)
			}
		}
		return records
	}
	previous, stopPrevious := startWebhookCoordinator(t, fixture, endpoint.URL, uuid.New(), time.Minute)
	defer stopPrevious()
	oldPasses := collect(previous, gate.ObservationPassing)
	stopPrevious()

	process := uuid.New()
	assertWaiting := func(want gate.RequirementState) {
		t.Helper()
		at := time.Now().UTC()
		runs, err := db.ListRunGateSummaries(ctx, []uuid.UUID{runID}, process, at)
		if err != nil {
			t.Fatal(err)
		}
		promotions, err := db.ListPromotionGateSummaries(ctx, []uuid.UUID{promotionID}, process, at)
		if err != nil {
			t.Fatal(err)
		}
		for _, requirements := range []gate.Summary{runs[runID], promotions[promotionID]} {
			if len(requirements) != 1 || requirements[0].State != want || requirements[0].Evidence != nil {
				t.Fatalf("expected %s without evidence, got %#v", want, requirements)
			}
			if want == gate.RequirementUnavailable && requirements[0].Diagnostic != "Webhook returned an unsuccessful HTTP status." {
				t.Fatalf("unexpected public diagnostic %q", requirements[0].Diagnostic)
			}
		}
		assertNoGateRunAssignment(t, db, process, at)
		if released, err := db.releasePromotionAt(ctx, promotionID, process, at); err != nil || released {
			t.Fatalf("released waiting Promotion = %t, %v", released, err)
		}
	}
	assertWaiting(gate.RequirementUnevaluated)
	available.Store(false)
	current, stop := startWebhookCoordinator(t, fixture, endpoint.URL, process, 2*time.Second)
	defer stop()
	failures := collect(current, gate.ObservationUnavailable)
	for _, old := range oldPasses {
		if err := db.UpsertGateObservation(ctx, old); err != nil {
			t.Fatal(err)
		}
	}
	assertWaiting(gate.RequirementUnavailable)

	available.Store(true)
	collect(current, gate.ObservationPassing)
	stop()
	for _, late := range failures {
		if err := db.UpsertGateObservation(ctx, late); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC()
	assigned, err := db.claimQueuedRunAt(ctx, "colo", testSchedulingCapacity(), process, at)
	if err != nil || assigned.ID != runID {
		t.Fatalf("claim recovered Run = %v, %v", assigned.ID, err)
	}
	if released, err := db.releasePromotionAt(ctx, promotionID, process, at); err != nil || !released {
		t.Fatalf("release recovered Promotion = %t, %v", released, err)
	}
}

func TestWebhookGateReleasesOnlyTheEvaluatedSubjectIntegration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	fixture, revision := newWebhookGateFixture(t, now)
	db, ctx, stage := fixture.database, fixture.ctx, fixture.stage
	blockedRun := createWebhookRun(t, fixture, revision, now.Add(-time.Second))
	passingRun := createWebhookRun(t, fixture, revision, now)
	passingPromotion := createWebhookPromotion(t, fixture, "pass", now)
	blockedPromotion := createWebhookPromotion(t, fixture, "wait", now)
	policyCommit := strings.Repeat("f", 40)
	active := activateWebhookGate(t, fixture, now, policyCommit, "updated-deploy")
	targetID := activeTargetID(t, db, fixture.repositoryID, "network")
	expected := map[uuid.UUID]string{
		blockedRun: "blocking", passingRun: "passing",
		blockedPromotion: "blocking", passingPromotion: "passing",
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Subject           gate.EvaluationSubject `json:"subject"`
			RepositoryID      uuid.UUID              `json:"repositoryId"`
			RepositoryName    string                 `json:"repositoryName"`
			CatalogRevisionID uuid.UUID              `json:"catalogRevisionId"`
			CommitSHA         string                 `json:"commitSha"`
			Input             map[string]string      `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		subject := request.Subject
		if request.RepositoryID != fixture.repositoryID || request.RepositoryName != "artifact-test" ||
			request.CatalogRevisionID != active || request.CommitSHA != policyCommit || request.Input["policy"] != "updated-deploy" {
			t.Errorf("unexpected active policy context %#v", request)
		}
		switch subject.Type {
		case gate.RunSubject:
			if subject.Run == nil || subject.Promotion != nil || subject.Run.TargetID != targetID ||
				subject.Run.TargetName != "network" || subject.Run.ActionName != "preview" || subject.Run.Intent != "preview" ||
				subject.Run.CatalogRevisionID != revision || subject.Run.CommitSHA != strings.Repeat("d", 40) {
				t.Error("incorrect Run context")
			}
		case gate.PromotionSubject:
			if subject.Promotion == nil || subject.Run != nil || subject.Promotion.StageID != stage.ID ||
				subject.Promotion.Cluster != stage.ClusterName || subject.Promotion.Project != stage.Project ||
				subject.Promotion.StageName != stage.Name || subject.Promotion.Integration != stage.Integration {
				t.Error("incorrect Promotion context")
			}
		default:
			t.Error("missing subject type")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"apiVersion":"slipramp.com/gate-evaluation/v1alpha1","decision":%q,"code":"policy","message":%q}`, expected[subject.ID], subject.ID.String())
	}))
	defer endpoint.Close()
	process := uuid.New()
	observations, stop := startWebhookCoordinator(t, fixture, endpoint.URL, process, time.Minute)
	defer stop()
	seen := map[uuid.UUID]bool{}
	var blocked, passing gate.CurrentObservation
	for range expected {
		select {
		case obs := <-observations:
			if obs.Observation.Evidence == nil {
				t.Fatalf("missing webhook evidence, observation %#v", obs)
			}
			if ev, ok := obs.Observation.Evidence.Payload.(*gate.WebhookEvidence); !ok || ev.Message != obs.SubjectID.String() ||
				expected[obs.SubjectID] != string(obs.Observation.State) || seen[obs.SubjectID] {
				t.Fatalf("unexpected webhook result %#v", obs)
			}
			seen[obs.SubjectID] = true
			if obs.SubjectID == passingRun {
				passing = obs
			}
			if obs.SubjectID == blockedPromotion {
				blocked = obs
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing subject-specific evaluation")
		}
	}
	stop()
	at := time.Now().UTC()
	assertNoGateRunAssignment(t, db, uuid.New(), at)
	assertWebhookUpdateSerializesWithClaim(t, db, passing, at)
	claimed, err := db.claimQueuedRunAt(ctx, "colo", testSchedulingCapacity(), process, at)
	if err != nil || claimed.ID != passingRun {
		t.Fatalf("claim with blocked head = %v, %v", claimed.ID, err)
	}
	release := func(id, processID uuid.UUID, want bool) {
		t.Helper()
		ok, err := db.releasePromotionAt(ctx, id, processID, at)
		if err != nil || ok != want {
			t.Fatalf("release %v = %t, %v, want %t", id, ok, err, want)
		}
	}
	release(passingPromotion, uuid.New(), false)
	release(passingPromotion, process, true)
	release(blockedPromotion, process, false)
	decision := gate.OverrideRequest{
		Subject:            gate.PromotionSubject,
		SubjectID:          blockedPromotion,
		Key:                gate.RepositoryKey(blocked.GateName),
		Version:            gate.Version(blocked.DefinitionDigest),
		Reason:             "Approved exception",
		Actor:              fixture.actor,
		EvaluatorProcessID: process,
		RecordedAt:         at,
	}
	projected, err := db.RecordGateOverride(ctx, decision)
	if err != nil || len(projected.Overrides) != 1 {
		t.Fatalf("webhook override = %#v, %v", projected, err)
	}
	assertWebhookSubject(t, projected.Overrides[0].ObservedEvidence, blockedPromotion)

	activateWebhookGate(t, fixture, at, strings.Repeat("e", 40), "revised")
	blocked.Observation.State = gate.ObservationPassing
	if err := db.UpsertGateObservation(ctx, blocked); err != nil {
		t.Fatal(err)
	}
	release(blockedPromotion, process, false)
	summaries, err := db.ListPromotionGateSummaries(ctx, []uuid.UUID{passingPromotion, blockedPromotion}, process, at)
	if err != nil {
		t.Fatal(err)
	}
	waiting := summaries[blockedPromotion][0]
	if waiting.State != gate.RequirementUnevaluated || waiting.Evidence != nil || waiting.Version == decision.Version {
		t.Fatalf("revised policy inherited evidence: %#v", waiting)
	}
	released := summaries[passingPromotion][0]
	if released.State != gate.RequirementReleased {
		t.Fatalf("released evidence changed: %#v", released)
	}
	assertWebhookSubject(t, released.Evidence, passingPromotion)
	summaries, err = db.ListRunGateSummaries(ctx, []uuid.UUID{passingRun, blockedRun}, process, at)
	if err != nil {
		t.Fatal(err)
	}
	assertWebhookSubject(t, summaries[passingRun][0].Evidence, passingRun)
	if summaries[blockedRun][0].State != gate.RequirementUnevaluated {
		t.Fatal("unexpected Run evidence after refresh")
	}
	cleared, err := db.DeleteInactiveGateObservations(ctx, at.Add(time.Hour))
	if err != nil || cleared != 2 {
		t.Fatalf("clean up old webhook observations = %d, %v", cleared, err)
	}
	if _, err := db.CancelRun(ctx, blockedRun, at, &fixture.actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.CancelPromotion(ctx, blockedPromotion, at, fixture.actor); err != nil {
		t.Fatal(err)
	}
	if demand, err := db.ListGateEvaluationDemand(ctx); err != nil || len(demand) != 0 {
		t.Fatalf("evaluation demand after release and cancellation = %#v, %v", demand, err)
	}
}

func assertWebhookSubject(t *testing.T, evidence *gate.Evidence, id uuid.UUID) {
	t.Helper()
	if evidence == nil {
		t.Fatal("missing webhook evidence")
	}
	payload, ok := evidence.Payload.(*gate.WebhookEvidence)
	if !ok || payload.Message != id.String() {
		t.Fatalf("expected evidence for %s, got %#v", id, evidence)
	}
}

// Hold an in-flight blocking decision on a previously passing Run. The scheduler
// must skip it until the observation writer commits or rolls back.
func assertWebhookUpdateSerializesWithClaim(t *testing.T, db *Database, record gate.CurrentObservation, at time.Time) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	writer := Database{queries: db.queries.WithTx(tx)}
	record.Observation.State = gate.ObservationBlocking
	record.EvaluatedAt = record.EvaluatedAt.Add(time.Microsecond)
	if err := writer.UpsertGateObservation(ctx, record); err != nil {
		t.Fatal(err)
	}
	_, err = db.claimQueuedRunAt(ctx, "colo", testSchedulingCapacity(), record.EvaluatorProcessID, at)
	if !errors.Is(err, run.ErrNoRunAssignment) {
		t.Fatalf("claim during webhook update = %v", err)
	}
}
