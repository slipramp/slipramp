package store

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/promotion"
)

func TestKargoPromotionGateReleaseIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.November, 20, 15, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	acceptedAt := createdAt.Add(2 * time.Minute)
	pollAt := acceptedAt.Add(30 * time.Second)
	evaluatorID := uuid.New()

	catalog := persistedCatalog("1.14.3")
	second := catalog.Gates[0]
	second.Metadata.Name = "window"
	catalog.Gates = append(catalog.Gates, second)
	refresh := successfulCatalogRefresh(strings.Repeat("d", 40), acceptedAt.Add(-time.Minute), uuid.New())
	refresh.Revision.Catalog = catalog
	if err := fixture.database.RecordRepositoryRefresh(fixture.ctx, fixture.repositoryID, refresh); err != nil {
		t.Fatal(err)
	}

	stage := fixture.stage
	stage.ResourceVersion = "2"
	stage.Gates = []string{"network-idle", "window"}
	stage.FirstObservedAt = acceptedAt.Add(-30 * time.Second)
	stage.LastObservedAt = acceptedAt.Add(-30 * time.Second)
	if err := fixture.database.ReplacePromotionStageSnapshot(
		fixture.ctx,
		promotionTestSnapshot(uuid.New(), stage.Integration, stage.ClusterName, stage.LastObservedAt),
		[]promotion.Stage{stage},
	); err != nil {
		t.Fatalf("attach Gate to callback Stage: %v", err)
	}

	record, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		fixture.acceptance(t, fixture.request("external-dns-gated"), acceptedAt),
	)
	if err != nil || record.Lifecycle.State != promotion.LifecycleApproved {
		t.Fatalf("accept automatic Promotion = %#v, %v", record, err)
	}
	released, err := fixture.database.ReleasePromotionGates(
		fixture.ctx,
		record.ID,
		evaluatorID,
		pollAt,
	)
	if err != nil || released {
		t.Fatalf("release without evidence = %t, %v", released, err)
	}

	targets, err := fixture.database.ListGateEvaluationDemand(fixture.ctx)
	if err != nil || len(targets) != 2 {
		t.Fatalf("Promotion Gate evaluation demand = %#v, %v", targets, err)
	}
	target := targets[0]
	digest, err := target.Definition.Digest()
	if err != nil {
		t.Fatalf("digest Promotion Gate definition: %v", err)
	}
	observedValue := 0.0
	observation := gate.CurrentObservation{
		RepositoryID:       fixture.repositoryID,
		GateName:           "network-idle",
		CatalogRevisionID:  target.CatalogRevisionID,
		DefinitionDigest:   digest,
		EvaluatorProcessID: evaluatorID,
		Observation: gate.Observation{
			State: gate.ObservationPassing,
			Value: &observedValue,
		},
		EvaluatedAt: acceptedAt,
		ValidUntil:  pollAt.Add(time.Minute),
	}
	if err := fixture.database.UpsertGateObservation(fixture.ctx, observation); err != nil {
		t.Fatalf("record passing Promotion Gate observation: %v", err)
	}
	released, err = fixture.database.ReleasePromotionGates(fixture.ctx, record.ID, evaluatorID, pollAt)
	if err != nil || released {
		t.Fatalf("release with one of two Gates passing = %t, %v", released, err)
	}
	service := promotion.NewReviewService(nil, fixture.database, evaluatorID, nil)
	summaries, err := service.GateSummaries(fixture.ctx, []promotion.Promotion{record}, pollAt)
	if err != nil {
		t.Fatal(err)
	}
	waiting := summaries[record.ID]
	wantDefinition := catalog.Gates[0].Spec.Prometheus
	if waiting.State() != gate.SummaryWaiting || len(waiting) != 2 ||
		waiting[0].Name != "network-idle" || waiting[0].State != gate.RequirementPassing ||
		waiting[0].Prometheus == nil || *waiting[0].Prometheus != wantDefinition ||
		waiting[1].Name != "window" || waiting[1].State != gate.RequirementUnevaluated {
		t.Fatalf("partial evidence was latched or reordered: %#v", waiting)
	}
	// The other Promotion can waive its own window while this one continues to wait.
	other, err := fixture.database.AcceptPromotionRequest(fixture.ctx,
		fixture.acceptance(t, fixture.request("emergency-deploy"), acceptedAt))
	if err != nil {
		t.Fatal(err)
	}
	request := gate.OverrideRequest{Subject: gate.PromotionSubject, SubjectID: other.ID, GateName: "window",
		DefinitionDigest: digest, Actor: fixture.actor, Reason: "Incident recovery", RecordedAt: pollAt,
		EvaluatorProcessID: evaluatorID}
	if err := fixture.database.RecordGateOverride(fixture.ctx, request); err != nil {
		t.Fatal(err)
	}
	if ok, err := fixture.database.ReleasePromotionGates(fixture.ctx, other.ID, evaluatorID, pollAt); !ok || err != nil {
		t.Fatalf("release with pass and override = %t, %v", ok, err)
	}
	if ok, err := fixture.database.ReleasePromotionGates(fixture.ctx, record.ID, evaluatorID, pollAt); ok || err != nil {
		t.Fatalf("override leaked to another Promotion: %t, %v", ok, err)
	}
	summaries, err = service.GateSummaries(fixture.ctx, []promotion.Promotion{other}, pollAt)
	if err != nil {
		t.Fatal(err)
	}
	mixed := summaries[other.ID]
	if mixed.State() != gate.SummaryReleased || len(mixed[0].Overrides) != 0 || mixed[0].Value == nil ||
		len(mixed[1].Overrides) != 1 || mixed[1].Overrides[0].ObservedState != gate.RequirementUnevaluated ||
		mixed[1].Value != nil || mixed[1].EvaluatedAt != nil {
		t.Fatalf("mixed Promotion release evidence = %#v", mixed)
	}
	window := observation
	window.GateName = "window"
	if err := fixture.database.UpsertGateObservation(fixture.ctx, window); err != nil {
		t.Fatal(err)
	}

	released, err = fixture.database.ReleasePromotionGates(
		fixture.ctx,
		record.ID,
		evaluatorID,
		pollAt,
	)
	if err != nil || !released {
		t.Fatalf("release with passing evidence = %t, %v", released, err)
	}
	summaries, err = service.GateSummaries(fixture.ctx, []promotion.Promotion{record}, pollAt)
	if err != nil {
		t.Fatalf("list released Promotion Gate projections: %v", err)
	}
	projections := summaries[record.ID]
	if projections.State() != gate.SummaryReleased || len(projections) != 2 || projections[0].ReleasedAt == nil ||
		!projections[0].ReleasedAt.Equal(pollAt) || projections[0].Prometheus == nil ||
		*projections[0].Prometheus != wantDefinition || projections[0].Value == nil ||
		*projections[0].Value != observedValue || projections[0].EvaluatedAt == nil ||
		!projections[0].EvaluatedAt.Equal(observation.EvaluatedAt) {
		t.Errorf("released Promotion Gate projection = %#v", projections)
	}

	var evidenceMatches bool
	err = fixture.database.pool.QueryRow(
		fixture.ctx,
		`SELECT
             released_at = $2
             AND release_catalog_revision_id = $3
             AND release_definition_digest = $4
             AND release_evaluator_process_id = $5
             AND release_observed_value = $6
             AND release_evaluated_at = $7
             AND release_valid_until = $8
         FROM promotion_gates
         WHERE promotion_id = $1 AND gate_name = 'network-idle'`,
		databaseUUID(record.ID),
		databaseTime(pollAt),
		databaseUUID(target.CatalogRevisionID),
		digest,
		databaseUUID(evaluatorID),
		observedValue,
		databaseTime(observation.EvaluatedAt),
		databaseTime(observation.ValidUntil),
	).Scan(&evidenceMatches)
	if err != nil || !evidenceMatches {
		t.Fatalf("persisted Promotion Gate evidence matches = %t, %v", evidenceMatches, err)
	}

	observation.Observation.State = gate.ObservationBlocking
	observation.EvaluatedAt = pollAt.Add(time.Second)
	if err := fixture.database.UpsertGateObservation(fixture.ctx, observation); err != nil {
		t.Fatalf("record post-release blocking observation: %v", err)
	}
	released, err = fixture.database.ReleasePromotionGates(
		fixture.ctx,
		record.ID,
		evaluatorID,
		pollAt.Add(2*time.Second),
	)
	if err != nil || !released {
		t.Fatalf("repeat latched release = %t, %v", released, err)
	}
	if targets, err := fixture.database.ListGateEvaluationDemand(fixture.ctx); err != nil || len(targets) != 0 {
		t.Errorf("Gate evaluation demand after callback release = %#v, %v", targets, err)
	}
}
