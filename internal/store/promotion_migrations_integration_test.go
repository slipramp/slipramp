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
