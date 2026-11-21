package store

import (
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

	stage := fixture.stage
	stage.ResourceVersion = "2"
	stage.Gates = []string{"network-idle"}
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
	projections, err := fixture.database.ListPromotionGateProjections(fixture.ctx, []uuid.UUID{record.ID})
	if err != nil {
		t.Fatalf("list unreleased Promotion Gate projections: %v", err)
	}
	wantDefinition := persistedCatalog("1.14.3").Gates[0].NormalizedDefinition()
	if len(projections) != 1 || projections[0].PromotionID != record.ID ||
		projections[0].Name != "network-idle" || projections[0].ActiveDefinition == nil ||
		*projections[0].ActiveDefinition != wantDefinition ||
		projections[0].CurrentObservation != nil || projections[0].ReleasedAt != nil {
		t.Errorf("unreleased Promotion Gate projection = %#v", projections)
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

	targets, err := fixture.database.ListGateEvaluationTargets(fixture.ctx)
	if err != nil || len(targets) != 1 {
		t.Fatalf("Promotion Gate evaluation targets = %#v, %v", targets, err)
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
	projections, err = fixture.database.ListPromotionGateProjections(fixture.ctx, []uuid.UUID{record.ID})
	if err != nil {
		t.Fatalf("list observed Promotion Gate projections: %v", err)
	}
	if len(projections) != 1 || projections[0].CurrentObservation == nil ||
		projections[0].CurrentObservation.EvaluatorProcessID != evaluatorID ||
		projections[0].CurrentObservation.Observation.Value == nil ||
		*projections[0].CurrentObservation.Observation.Value != observedValue {
		t.Errorf("observed Promotion Gate projection = %#v", projections)
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
	projections, err = fixture.database.ListPromotionGateProjections(fixture.ctx, []uuid.UUID{record.ID})
	if err != nil {
		t.Fatalf("list released Promotion Gate projections: %v", err)
	}
	if len(projections) != 1 || projections[0].ReleasedAt == nil ||
		!projections[0].ReleasedAt.Equal(pollAt) || projections[0].ReleaseDefinition == nil ||
		*projections[0].ReleaseDefinition != wantDefinition || projections[0].ReleaseValue == nil ||
		*projections[0].ReleaseValue != observedValue || projections[0].ReleaseEvaluatedAt == nil ||
		!projections[0].ReleaseEvaluatedAt.Equal(observation.EvaluatedAt) {
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
	if targets, err := fixture.database.ListGateEvaluationTargets(fixture.ctx); err != nil || len(targets) != 0 {
		t.Errorf("Gate evaluation targets after callback release = %#v, %v", targets, err)
	}
}
