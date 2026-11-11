package store

import (
	"io/fs"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestKargoStageSnapshotCheckpointMigrationRetainsLatestPerIntegrationClusterIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	databaseAdapter := stdlib.OpenDBFromPool(database.pool)
	defer func() {
		if err := databaseAdapter.Close(); err != nil {
			t.Errorf("close migration database adapter: %v", err)
		}
	}()
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		databaseAdapter,
		migrations,
		goose.WithTableName(migrationTable),
	)
	if err != nil {
		t.Fatalf("create migration provider: %v", err)
	}
	if _, err := provider.DownTo(ctx, 54); err != nil {
		t.Fatalf("roll back Kargo Stage snapshot checkpoint migration: %v", err)
	}

	base := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	wantSnapshotID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c2104")
	rows := []struct {
		id          uuid.UUID
		integration string
		cluster     string
		observedAt  time.Time
		acceptedAt  time.Time
	}{
		{uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c2101"), "prod", "colo", base, base.Add(time.Second)},
		{
			uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c2102"),
			"prod",
			"colo",
			base.Add(time.Minute),
			base.Add(time.Minute + time.Second),
		},
		{
			uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c2103"),
			"prod",
			"colo",
			base.Add(time.Minute),
			base.Add(time.Minute + 2*time.Second),
		},
		{wantSnapshotID, "prod", "colo", base.Add(time.Minute), base.Add(time.Minute + 2*time.Second)},
		{
			uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c2105"),
			"edge",
			"edge",
			base.Add(2 * time.Minute),
			base.Add(2*time.Minute + time.Second),
		},
	}
	for _, row := range rows {
		if _, err := database.pool.Exec(
			ctx,
			`INSERT INTO kargo_stage_snapshots
                (snapshot_id, integration, cluster_name, observed_at, accepted_at)
             VALUES ($1, $2, $3, $4, $5)`,
			row.id,
			row.integration,
			row.cluster,
			row.observedAt,
			row.acceptedAt,
		); err != nil {
			t.Fatalf("insert legacy Kargo Stage snapshot %s: %v", row.id, err)
		}
	}

	if _, err := provider.UpTo(ctx, 55); err != nil {
		t.Fatalf("apply Kargo Stage snapshot checkpoint migration: %v", err)
	}

	var snapshotID uuid.UUID
	var observedAt time.Time
	var acceptedAt time.Time
	if err := database.pool.QueryRow(
		ctx,
		`SELECT snapshot_id, observed_at, accepted_at
         FROM kargo_stage_snapshots
         WHERE integration = 'prod' AND cluster_name = 'colo'`,
	).Scan(&snapshotID, &observedAt, &acceptedAt); err != nil {
		t.Fatalf("load migrated Kargo Stage snapshot checkpoint: %v", err)
	}
	if snapshotID != wantSnapshotID || !observedAt.Equal(base.Add(time.Minute)) ||
		!acceptedAt.Equal(base.Add(time.Minute+2*time.Second)) {
		t.Errorf(
			"migrated checkpoint = %s at %v accepted %v",
			snapshotID,
			observedAt,
			acceptedAt,
		)
	}

	var checkpointCount int
	if err := database.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM kargo_stage_snapshots`,
	).Scan(&checkpointCount); err != nil {
		t.Fatalf("count migrated Kargo Stage snapshot checkpoints: %v", err)
	}
	if checkpointCount != 2 {
		t.Errorf("migrated Kargo Stage snapshot checkpoint count = %d, want 2", checkpointCount)
	}

	var indexCount int
	var legacyIndexCount int
	if err := database.pool.QueryRow(
		ctx,
		`SELECT count(*), count(*) FILTER (WHERE indexname = 'kargo_stage_snapshots_latest')
         FROM pg_indexes
         WHERE schemaname = 'public' AND tablename = 'kargo_stage_snapshots'`,
	).Scan(&indexCount, &legacyIndexCount); err != nil {
		t.Fatalf("inspect Kargo Stage snapshot indexes: %v", err)
	}
	if indexCount != 2 || legacyIndexCount != 0 {
		t.Errorf("Kargo Stage snapshot indexes = %d total, %d legacy", indexCount, legacyIndexCount)
	}

	_, err = database.pool.Exec(
		ctx,
		`INSERT INTO kargo_stage_snapshots
            (snapshot_id, integration, cluster_name, observed_at, accepted_at)
         VALUES ($1, 'prod', 'colo', $2, $3)`,
		uuid.New(),
		base.Add(3*time.Minute),
		base.Add(3*time.Minute+time.Second),
	)
	if !constraintMatches(err, "kargo_stage_snapshots_pkey") {
		t.Errorf("duplicate checkpoint error = %v, want checkpoint primary key", err)
	}

	_, err = database.pool.Exec(
		ctx,
		`INSERT INTO kargo_stage_snapshots
            (snapshot_id, integration, cluster_name, observed_at, accepted_at)
         VALUES ($1, 'other', 'other', $2, $3)`,
		wantSnapshotID,
		base.Add(3*time.Minute),
		base.Add(3*time.Minute+time.Second),
	)
	if !constraintMatches(err, "kargo_stage_snapshots_snapshot_id_key") {
		t.Errorf("duplicate snapshot ID error = %v, want snapshot ID unique constraint", err)
	}
}

func TestMetadataFreeKargoPromotionMigrationPreservesLegacyDispatchFailureIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 24, 1, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	stage := fixture.stage
	stage.ResourceVersion = "legacy-dispatch-failure"
	stage.Freight = promotion.Freight{Items: []promotion.FreightItem{{
		Origin:    "Warehouse/external-dns",
		Name:      "freight-42",
		Artifacts: []promotion.FreightArtifact{{Kind: "image", Version: "1.2.3"}},
	}}}
	stage.FirstObservedAt = createdAt.Add(2 * time.Minute)
	stage.LastObservedAt = createdAt.Add(2 * time.Minute)
	if err := fixture.database.ReplacePromotionStageSnapshot(
		fixture.ctx,
		promotionTestSnapshot(uuid.New(), stage.Integration, stage.ClusterName, stage.LastObservedAt),
		[]promotion.Stage{stage},
	); err != nil {
		t.Fatalf("replace legacy dispatch Stage snapshot: %v", err)
	}

	record, err := fixture.database.CreateOperatorPromotion(
		fixture.ctx,
		promotion.OperatorPromotionAcceptance{
			ID:                         uuid.New(),
			Integration:                stage.Integration,
			TargetStageID:              stage.ID,
			SourceStageID:              stage.ID,
			FreightName:                "freight-42",
			AllowedRepositories:        []string{stage.RepositoryName},
			RequestedByPrincipalID:     fixture.principal.ID,
			RequestedBySnapshot:        fixture.actor,
			AcceptedAt:                 createdAt.Add(3 * time.Minute),
			MaximumStageObservationAge: 5 * time.Minute,
		},
	)
	if err != nil {
		t.Fatalf("create legacy Promotion: %v", err)
	}
	if record.ReviewContext == nil {
		t.Fatal("legacy Promotion review context is nil")
	}
	claimed, err := fixture.database.ClaimPromotionCreation(fixture.ctx, promotion.CreationDispatchClaim{
		ClusterName:      stage.ClusterName,
		ClaimedAt:        record.CreatedAt,
		RetryDelays:      []time.Duration{time.Minute},
		ExhaustedMessage: "dispatch attempts exhausted",
	})
	if err != nil {
		t.Fatalf("claim legacy Promotion creation: %v", err)
	}
	diagnostic := "Kargo admission rejected the Promotion"
	failedAt := record.CreatedAt.Add(time.Second)
	failed, err := fixture.database.ReportPromotionCreation(fixture.ctx, promotion.CreationDispatchReport{
		PromotionID: record.ID,
		ClusterName: stage.ClusterName,
		Attempt:     claimed.CreationDispatch.AttemptCount,
		Result:      promotion.CreationDispatchFailed,
		Diagnostic:  &diagnostic,
		ReportedAt:  failedAt,
	})
	if err != nil {
		t.Fatalf("fail legacy Promotion creation: %v", err)
	}
	if failed.CreationDispatch.FailedAt == nil {
		t.Fatal("legacy Promotion creation failure was not recorded")
	}

	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	databaseAdapter := stdlib.OpenDBFromPool(fixture.database.pool)
	defer func() {
		if err := databaseAdapter.Close(); err != nil {
			t.Errorf("close migration database adapter: %v", err)
		}
	}()
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		databaseAdapter,
		migrations,
		goose.WithTableName(migrationTable),
	)
	if err != nil {
		t.Fatalf("create migration provider: %v", err)
	}
	if _, err := provider.DownTo(fixture.ctx, 52); err != nil {
		t.Fatalf("roll back metadata-free Kargo Promotion migration: %v", err)
	}
	if _, err := provider.UpTo(fixture.ctx, 53); err != nil {
		t.Fatalf("apply metadata-free Kargo Promotion migration: %v", err)
	}

	var status string
	var message *string
	var storedFailedAt *time.Time
	var storedDiagnostic *string
	if err := fixture.database.pool.QueryRow(
		fixture.ctx,
		`SELECT status, message, dispatch_failed_at, dispatch_diagnostic
         FROM promotions
         WHERE id = $1`,
		record.ID,
	).Scan(&status, &message, &storedFailedAt, &storedDiagnostic); err != nil {
		t.Fatalf("load migrated Promotion: %v", err)
	}
	if status != string(promotion.LifecycleApproved) || message != nil || storedFailedAt == nil ||
		!storedFailedAt.Equal(failedAt) || storedDiagnostic == nil || *storedDiagnostic != diagnostic {
		t.Errorf(
			"migrated Promotion = status %q, message %v, failed at %v, diagnostic %v",
			status,
			message,
			storedFailedAt,
			storedDiagnostic,
		)
	}
}
