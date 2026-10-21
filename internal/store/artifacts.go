package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const (
	artifactsPrimaryKeyConstraint    = "artifacts_pkey"
	artifactsRunForeignKeyConstraint = "artifacts_run_id_fkey"
	artifactsRunNameConstraint       = "artifacts_run_name_key"
	artifactsObjectKeyConstraint     = "artifacts_content_object_key_key"
)

var (
	_ artifact.RunStore       = (*Database)(nil)
	_ artifact.ArtifactStore  = (*Database)(nil)
	_ artifact.LogChunkStore  = (*Database)(nil)
	_ artifact.RetentionStore = (*Database)(nil)
)

func (d *Database) GetRunClusterName(ctx context.Context, runID uuid.UUID) (string, error) {
	clusterName, err := d.queries.GetArtifactRunClusterName(ctx, databaseUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", artifact.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get artifact Run cluster: %w", err)
	}

	return clusterName, nil
}

func (d *Database) CreateArtifact(ctx context.Context, record artifact.Artifact) (artifact.Artifact, error) {
	if err := record.Validate(); err != nil {
		return artifact.Artifact{}, err
	}

	row, err := d.queries.CreateArtifact(
		ctx,
		storedb.CreateArtifactParams{
			ID:               databaseUUID(record.ID),
			RunID:            databaseUUID(record.RunID),
			Name:             record.Name,
			Kind:             string(record.Kind),
			State:            string(record.State),
			MediaType:        record.MediaType,
			SizeBytes:        record.SizeBytes,
			Sha256:           record.SHA256,
			Sensitive:        record.Sensitive,
			ContentObjectKey: record.ObjectKey,
			RetentionState:   string(record.RetentionState),
			CreatedAt:        databaseTime(record.CreatedAt),
			SealedAt:         optionalDatabaseTime(record.SealedAt),
		},
	)
	if err != nil {
		if constraintMatches(
			err,
			artifactsPrimaryKeyConstraint,
			artifactsRunNameConstraint,
			artifactsObjectKeyConstraint,
		) {
			return artifact.Artifact{}, artifact.ErrAlreadyExists
		}
		if constraintMatches(err, artifactsRunForeignKeyConstraint) {
			return artifact.Artifact{}, artifact.ErrNotFound
		}

		return artifact.Artifact{}, fmt.Errorf("create artifact: %w", err)
	}

	return artifactFromRow(row), nil
}

func (d *Database) GetArtifact(ctx context.Context, artifactID uuid.UUID) (artifact.Artifact, error) {
	row, err := d.queries.GetArtifact(ctx, databaseUUID(artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.Artifact{}, artifact.ErrNotFound
	}
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("get artifact: %w", err)
	}

	return artifactFromRow(row), nil
}

func (d *Database) GetArtifactByRunAndName(
	ctx context.Context,
	runID uuid.UUID,
	name string,
) (artifact.Artifact, error) {
	row, err := d.queries.GetArtifactByRunAndName(
		ctx,
		storedb.GetArtifactByRunAndNameParams{
			RunID: databaseUUID(runID),
			Name:  name,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.Artifact{}, artifact.ErrNotFound
	}
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("get artifact by Run and name: %w", err)
	}

	return artifactFromRow(row), nil
}

func (d *Database) ListArtifactsForRun(ctx context.Context, runID uuid.UUID) ([]artifact.Artifact, error) {
	rows, err := d.queries.ListArtifactsForRun(ctx, databaseUUID(runID))
	if err != nil {
		return nil, fmt.Errorf("list artifacts for Run: %w", err)
	}

	result := make([]artifact.Artifact, 0, len(rows))
	for _, row := range rows {
		result = append(result, artifactFromRow(row))
	}

	return result, nil
}

func (d *Database) GetArtifactProvenance(ctx context.Context, runID uuid.UUID) (artifact.Provenance, error) {
	row, err := d.queries.GetArtifactProvenance(ctx, databaseUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.Provenance{}, artifact.ErrNotFound
	}
	if err != nil {
		return artifact.Provenance{}, fmt.Errorf("get artifact provenance: %w", err)
	}

	return artifact.Provenance{
		ProducerRunID:     runID,
		RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
		CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
		CommitSHA:         row.CommitSha,
		TargetName:        row.TargetName,
		ActionName:        row.ActionName,
	}, nil
}

func (d *Database) GetRunArtifactContext(ctx context.Context, runID uuid.UUID) (artifact.RunContext, error) {
	row, err := d.queries.GetRunArtifactContext(ctx, databaseUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.RunContext{}, artifact.ErrNotFound
	}
	if err != nil {
		return artifact.RunContext{}, fmt.Errorf("get Run artifact context: %w", err)
	}

	var snapshot run.Snapshot
	if err := json.Unmarshal(row.Snapshot, &snapshot); err != nil {
		return artifact.RunContext{}, fmt.Errorf("decode Run artifact context: %w", err)
	}
	declarations := make(map[string]artifact.Declaration, len(snapshot.Action.Artifacts))
	for name, declaration := range snapshot.Action.Artifacts {
		declarations[name] = artifact.Declaration{
			MediaType: declaration.MediaType,
			Sensitive: declaration.Sensitive,
		}
	}

	return artifact.RunContext{
		ClusterName: row.ClusterName,
		Artifacts:   declarations,
	}, nil
}

func (d *Database) GetRunInputArtifact(
	ctx context.Context,
	runID uuid.UUID,
	inputName string,
) (artifact.Artifact, error) {
	row, err := d.queries.GetRunInputArtifact(
		ctx,
		storedb.GetRunInputArtifactParams{
			RunID:     databaseUUID(runID),
			InputName: inputName,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.Artifact{}, artifact.ErrNotFound
	}
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("get Run input artifact: %w", err)
	}

	return artifactFromRow(row), nil
}

func artifactFromRow(row storedb.Artifact) artifact.Artifact {
	return artifact.Artifact{
		ID:             uuid.UUID(row.ID.Bytes),
		RunID:          uuid.UUID(row.RunID.Bytes),
		Name:           row.Name,
		Kind:           artifact.Kind(row.Kind),
		State:          artifact.State(row.State),
		MediaType:      row.MediaType,
		SizeBytes:      row.SizeBytes,
		SHA256:         row.Sha256,
		Sensitive:      row.Sensitive,
		ObjectKey:      row.ContentObjectKey,
		RetentionState: artifact.RetentionState(row.RetentionState),
		CreatedAt:      row.CreatedAt.Time.UTC(),
		SealedAt:       applicationTime(row.SealedAt),
	}
}
