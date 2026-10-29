package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/runreport"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const (
	artifactReportsPrimaryKeyConstraint          = "artifact_reports_pkey"
	artifactReportsArtifactForeignKeyConstraint  = "artifact_reports_artifact_id_fkey"
	artifactReportsProjectionObjectKeyConstraint = "artifact_reports_projection_object_key_key"
)

func (d *Database) CreateArtifactWithReport(
	ctx context.Context,
	record artifact.Artifact,
	report runreport.ArtifactReport,
) (artifact.Artifact, error) {
	if err := record.Validate(); err != nil {
		return artifact.Artifact{}, err
	}
	if err := report.Validate(); err != nil {
		return artifact.Artifact{}, err
	}
	if report.ArtifactID != record.ID {
		return artifact.Artifact{}, fmt.Errorf("%w: report Artifact ID does not match", artifact.ErrInvalid)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("begin artifact report creation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	created, err := createArtifact(ctx, queries, record)
	if err != nil {
		return artifact.Artifact{}, err
	}
	if err := createArtifactReport(ctx, queries, report); err != nil {
		return artifact.Artifact{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return artifact.Artifact{}, fmt.Errorf("commit artifact report creation: %w", err)
	}

	return created, nil
}

func (d *Database) GetArtifactReport(
	ctx context.Context,
	artifactID uuid.UUID,
) (runreport.ArtifactReport, error) {
	row, err := d.queries.GetArtifactReport(ctx, databaseUUID(artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return runreport.ArtifactReport{}, artifact.ErrNotFound
	}
	if err != nil {
		return runreport.ArtifactReport{}, fmt.Errorf("get artifact report: %w", err)
	}

	return artifactReportFromRow(row)
}

func (d *Database) ListArtifactReportsForRun(
	ctx context.Context,
	runID uuid.UUID,
) ([]runreport.ArtifactReport, error) {
	rows, err := d.queries.ListArtifactReportsForRun(ctx, databaseUUID(runID))
	if err != nil {
		return nil, fmt.Errorf("list artifact reports for Run: %w", err)
	}

	result := make([]runreport.ArtifactReport, 0, len(rows))
	for _, row := range rows {
		record, err := artifactReportFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}

	return result, nil
}

func createArtifactReport(
	ctx context.Context,
	queries *storedb.Queries,
	report runreport.ArtifactReport,
) error {
	summary, err := runreport.EncodeSummary(report.Summary)
	if err != nil {
		return err
	}

	_, err = queries.CreateArtifactReport(ctx, storedb.CreateArtifactReportParams{
		ArtifactID:              databaseUUID(report.ArtifactID),
		Kind:                    string(report.Kind),
		ProjectionSchemaVersion: int32(report.SchemaVersion),
		AdapterVersion:          int32(report.AdapterVersion),
		Tool:                    report.Tool,
		ToolVersion:             optionalString(report.ToolVersion),
		Mode:                    string(report.Mode),
		Summary:                 summary,
		ProjectionObjectKey:     report.ProjectionObjectKey,
		ProjectionSizeBytes:     report.ProjectionSizeBytes,
		ProjectionSha256:        report.ProjectionSHA256,
		CreatedAt:               databaseTime(report.CreatedAt),
	})
	if constraintMatches(
		err,
		artifactReportsPrimaryKeyConstraint,
		artifactReportsProjectionObjectKeyConstraint,
	) {
		return artifact.ErrAlreadyExists
	}
	if constraintMatches(err, artifactReportsArtifactForeignKeyConstraint) {
		return artifact.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("create artifact report: %w", err)
	}

	return nil
}

func artifactReportFromRow(row storedb.ArtifactReport) (runreport.ArtifactReport, error) {
	kind := runreport.ReportKind(row.Kind)
	summary, err := runreport.DecodeSummary(kind, row.Summary)
	if err != nil {
		return runreport.ArtifactReport{}, fmt.Errorf("decode stored artifact report summary: %w", err)
	}

	record := runreport.ArtifactReport{
		ArtifactID:          uuid.UUID(row.ArtifactID.Bytes),
		Kind:                kind,
		SchemaVersion:       int(row.ProjectionSchemaVersion),
		AdapterVersion:      int(row.AdapterVersion),
		Tool:                row.Tool,
		ToolVersion:         stringValue(row.ToolVersion),
		Mode:                runreport.ReportMode(row.Mode),
		Summary:             summary,
		ProjectionObjectKey: row.ProjectionObjectKey,
		ProjectionSizeBytes: row.ProjectionSizeBytes,
		ProjectionSHA256:    row.ProjectionSha256,
		CreatedAt:           row.CreatedAt.Time.UTC(),
	}
	if err := record.Validate(); err != nil {
		return runreport.ArtifactReport{}, fmt.Errorf("validate stored artifact report: %w", err)
	}

	return record, nil
}
