package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const (
	artifactsPrimaryKeyConstraint         = "artifacts_pkey"
	artifactsRunForeignKeyConstraint      = "artifacts_run_id_fkey"
	artifactsRunNameConstraint            = "artifacts_run_name_key"
	artifactsObjectKeyConstraint          = "artifacts_content_object_key_key"
	artifactLogChunksPrimaryKeyConstraint = "artifact_log_chunks_pkey"
	artifactLogChunksForeignKeyConstraint = "artifact_log_chunks_artifact_id_fkey"
	artifactLogChunksObjectKeyConstraint  = "artifact_log_chunks_object_key_key"
)

var _ artifact.Store = (*Database)(nil)

func (d *Database) CreateArtifact(
	ctx context.Context,
	record artifact.Artifact,
) (artifact.Artifact, error) {
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
			SealedAt:         databaseOptionalTime(record.SealedAt),
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

func (d *Database) GetArtifact(
	ctx context.Context,
	artifactID uuid.UUID,
) (artifact.Artifact, error) {
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

func (d *Database) ListArtifactsForRun(
	ctx context.Context,
	runID uuid.UUID,
) ([]artifact.Artifact, error) {
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

func (d *Database) CreateLogChunk(
	ctx context.Context,
	chunk artifact.LogChunk,
) (artifact.LogChunk, error) {
	if err := chunk.Validate(); err != nil {
		return artifact.LogChunk{}, err
	}

	row, err := d.queries.CreateArtifactLogChunk(
		ctx,
		storedb.CreateArtifactLogChunkParams{
			ArtifactID:  databaseUUID(chunk.ArtifactID),
			OffsetBytes: chunk.OffsetBytes,
			SizeBytes:   chunk.SizeBytes,
			Sha256:      chunk.SHA256,
			ObjectKey:   chunk.ObjectKey,
			CreatedAt:   databaseTime(chunk.CreatedAt),
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return artifact.LogChunk{}, artifact.ErrNotFound
		}
		if constraintMatches(
			err,
			artifactLogChunksPrimaryKeyConstraint,
			artifactLogChunksObjectKeyConstraint,
		) {
			return artifact.LogChunk{}, artifact.ErrAlreadyExists
		}
		if constraintMatches(err, artifactLogChunksForeignKeyConstraint) {
			return artifact.LogChunk{}, artifact.ErrNotFound
		}

		return artifact.LogChunk{}, fmt.Errorf("create artifact log chunk: %w", err)
	}

	return logChunkFromRow(row), nil
}

func (d *Database) GetLogChunk(
	ctx context.Context,
	artifactID uuid.UUID,
	offsetBytes int64,
) (artifact.LogChunk, error) {
	if offsetBytes < 0 {
		return artifact.LogChunk{}, fmt.Errorf(
			"%w: chunk offset must not be negative",
			artifact.ErrInvalid,
		)
	}

	row, err := d.queries.GetArtifactLogChunk(
		ctx,
		storedb.GetArtifactLogChunkParams{
			ArtifactID:  databaseUUID(artifactID),
			OffsetBytes: offsetBytes,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.LogChunk{}, artifact.ErrNotFound
	}
	if err != nil {
		return artifact.LogChunk{}, fmt.Errorf("get artifact log chunk: %w", err)
	}

	return logChunkFromRow(row), nil
}

func (d *Database) ListLogChunks(
	ctx context.Context,
	artifactID uuid.UUID,
	fromOffsetBytes int64,
) ([]artifact.LogChunk, error) {
	if fromOffsetBytes < 0 {
		return nil, fmt.Errorf(
			"%w: starting chunk offset must not be negative",
			artifact.ErrInvalid,
		)
	}

	rows, err := d.queries.ListArtifactLogChunks(
		ctx,
		storedb.ListArtifactLogChunksParams{
			ArtifactID:      databaseUUID(artifactID),
			FromOffsetBytes: fromOffsetBytes,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list artifact log chunks: %w", err)
	}

	result := make([]artifact.LogChunk, 0, len(rows))
	for _, row := range rows {
		result = append(result, logChunkFromRow(row))
	}

	return result, nil
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

func logChunkFromRow(row storedb.ArtifactLogChunk) artifact.LogChunk {
	return artifact.LogChunk{
		ArtifactID:  uuid.UUID(row.ArtifactID.Bytes),
		OffsetBytes: row.OffsetBytes,
		SizeBytes:   row.SizeBytes,
		SHA256:      row.Sha256,
		ObjectKey:   row.ObjectKey,
		CreatedAt:   row.CreatedAt.Time.UTC(),
	}
}

func databaseOptionalTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}

	return databaseTime(*value)
}

func constraintMatches(err error, constraints ...string) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}

	return slices.Contains(constraints, postgresError.ConstraintName)
}
