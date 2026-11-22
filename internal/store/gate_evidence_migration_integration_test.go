package store

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/slipramp/slipramp/internal/gate"
)

func TestGateEvidenceMigrationPreservesHistoryIntegration(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	fixture := newPromotionIntegrationFixture(t, at.Add(-2*time.Minute))
	ctx, db := fixture.ctx, fixture.database
	process := uuid.New()
	refresh := successfulCatalogRefresh(strings.Repeat("d", 40), at, uuid.New())
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	if err := db.RecordRepositoryRefresh(ctx, fixture.repositoryID, refresh); err != nil {
		t.Fatal(err)
	}
	name := refresh.Revision.Catalog.Gates[0].Metadata.Name
	digest, err := refresh.Revision.Catalog.Gates[0].Spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	createRun := func() uuid.UUID {
		t.Helper()
		record := gateTestRun(t, db, fixture.repositoryID, refresh.Revision.ID, at)
		if _, err := db.CreateRun(ctx, record, nil); err != nil {
			t.Fatal(err)
		}
		return record.ID
	}
	releasedRun, waitingRun := createRun(), createRun()
	prom, err := db.AcceptPromotionRequest(ctx, fixture.acceptance(t, fixture.request("migration"), at))
	if err != nil {
		t.Fatal(err)
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
	if _, err := provider.DownTo(ctx, 72); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Seed the pre-upgrade schema directly, including a zero release, a fractional
	// live sample, and ordered override decisions with and without an observation.
	exec(`INSERT INTO gate_observations
		(repository_id,gate_name,catalog_revision_id,definition_digest,evaluator_process_id,state,value,evaluated_at,valid_until)
		VALUES ($1,$2,$3,$4,$5,'blocking',-0.125,$6,$7)`,
		fixture.repositoryID, name, refresh.Revision.ID, digest, process, at, at.Add(time.Minute))
	exec(`UPDATE run_gates SET released_at=$2, release_catalog_revision_id=$3,
		release_definition=(SELECT spec FROM gate_evaluation_demand WHERE repository_id=$4 AND gate_name=$5),
		release_definition_digest=$6, release_evaluator_process_id=$7, release_observed_value=0,
		release_evaluated_at=$2, release_valid_until=$8 WHERE run_id=$1`,
		releasedRun, at, refresh.Revision.ID, fixture.repositoryID, name, digest, process, at.Add(time.Minute))
	exec(`INSERT INTO promotion_gates
		(promotion_id,repository_id,gate_name,attachment_order,released_at,release_catalog_revision_id,
		 release_definition,release_definition_digest,overrides)
		SELECT $1,$2,$3,0,$4,$5,spec,$6,jsonb_build_array(
			jsonb_build_object('definitionDigest',$6::text,'reason','sampled','observedState','blocking','observedValue',0),
			jsonb_build_object('definitionDigest',$6::text,'reason','waiting','observedState','unevaluated'))
		FROM gate_evaluation_demand WHERE repository_id=$2 AND gate_name=$3`,
		prom.ID, fixture.repositoryID, name, at, refresh.Revision.ID, digest)
	exec(`INSERT INTO promotion_gates (promotion_id,repository_id,gate_name,attachment_order,released_at,
		release_catalog_revision_id,release_definition,release_definition_digest,release_evaluator_process_id,
		release_observed_value,release_evaluated_at,release_valid_until)
		SELECT $1,$2,'second',1,$3,$4,spec,$5,$6,1.5,$3,$7 FROM gate_evaluation_demand
		WHERE repository_id=$2 AND gate_name=$8`,
		prom.ID, fixture.repositoryID, at, refresh.Revision.ID, digest, process, at.Add(time.Minute), name)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	runs, err := db.ListRunGateSummaries(ctx, []uuid.UUID{releasedRun, waitingRun}, process, at)
	if err != nil {
		t.Fatal(err)
	}
	assertSample := func(evidence *gate.Evidence, want float64) {
		t.Helper()
		if value := evidence.PrometheusValue(); value == nil || *value != want {
			t.Fatalf("sample = %v, want %v", value, want)
		}
	}
	history := runs[releasedRun][0]
	if history.State != gate.RequirementReleased || history.Version != gate.Version(digest) || !history.ReleasedAt.Equal(at) ||
		history.ValidUntil == nil || !history.ValidUntil.Equal(at.Add(time.Minute)) {
		t.Fatalf("released Run history = %#v", history)
	}
	assertSample(history.Evidence, 0)
	live := runs[waitingRun][0]
	if live.State != gate.RequirementBlocking || !live.EvaluatedAt.Equal(at) {
		t.Fatalf("live Run evidence = %#v", live)
	}
	assertSample(live.Evidence, -0.125)
	promotions, err := db.ListPromotionGateSummaries(ctx, []uuid.UUID{prom.ID}, uuid.New(), at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	historical := promotions[prom.ID]
	if historical.State() != gate.SummaryReleased || len(historical) != 2 {
		t.Fatalf("Promotion history = %#v", historical)
	}
	waived := historical[0]
	if waived.Evidence != nil || waived.EvaluatedAt != nil || waived.ValidUntil != nil || len(waived.Overrides) != 2 ||
		waived.Overrides[0].Reference != waived.Reference || waived.Overrides[1].Reference != waived.Reference ||
		waived.Scope.ID != fixture.repositoryID || waived.Key != gate.RepositoryKey(name) || waived.Type != gate.PrometheusType ||
		waived.Overrides[0].Reason != "sampled" || waived.Overrides[1].Reason != "waiting" || waived.Overrides[1].ObservedEvidence != nil {
		t.Fatalf("override history = %#v", waived)
	}
	assertSample(waived.Overrides[0].ObservedEvidence, 0)
	assertSample(historical[1].Evidence, 1.5)

	for _, malformed := range []string{`null`, `{"unknown":{}}`, `{"prometheus":{"value":"NaN"}}`,
		`{"prometheus":{"value":1e400}}`, `{"prometheus":{"value":0,"labels":{}}}`} {
		_, err := db.pool.Exec(ctx, `UPDATE gate_observations SET evidence=$1 WHERE repository_id=$2`, malformed, fixture.repositoryID)
		if err == nil {
			t.Errorf("database accepted malformed evidence %s", malformed)
		}
	}
}
