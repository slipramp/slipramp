package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/run"
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

func (d *Database) GetRunClusterName(
	ctx context.Context,
	runID uuid.UUID,
) (string, error) {
	clusterName, err := d.queries.GetArtifactRunClusterName(
		ctx,
		databaseUUID(runID),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", artifact.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get artifact Run cluster: %w", err)
	}

	return clusterName, nil
}

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

func (d *Database) GetArtifactProvenance(
	ctx context.Context,
	runID uuid.UUID,
) (artifact.Provenance, error) {
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

func (d *Database) GetRunArtifactContext(
	ctx context.Context,
	runID uuid.UUID,
) (artifact.RunContext, error) {
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
	declarations := make(
		map[string]artifact.Declaration,
		len(snapshot.Action.Artifacts),
	)
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

func (d *Database) CreateLogChunk(
	ctx context.Context,
	chunk artifact.LogChunk,
) (artifact.LogChunk, error) {
	if err := chunk.Validate(); err != nil {
		return artifact.LogChunk{}, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return artifact.LogChunk{}, fmt.Errorf("begin log chunk transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)

	record, err := queries.LockArtifactForLogAppend(
		ctx,
		databaseUUID(chunk.ArtifactID),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.LogChunk{}, artifact.ErrNotFound
	}
	if err != nil {
		return artifact.LogChunk{}, fmt.Errorf("lock log artifact: %w", err)
	}
	if record.State != string(artifact.StateOpen) {
		return artifact.LogChunk{}, artifact.ErrLogClosed
	}
	if record.SizeBytes > chunk.OffsetBytes {
		return artifact.LogChunk{}, artifact.ErrAlreadyExists
	}
	if record.SizeBytes != chunk.OffsetBytes {
		return artifact.LogChunk{}, artifact.ErrChunkConflict
	}

	row, err := queries.CreateArtifactLogChunk(
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
	if err := queries.AdvanceArtifactLogSize(
		ctx,
		storedb.AdvanceArtifactLogSizeParams{
			NextOffsetBytes: chunk.EndOffset(),
			ID:              databaseUUID(chunk.ArtifactID),
		},
	); err != nil {
		return artifact.LogChunk{}, fmt.Errorf("advance log artifact size: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return artifact.LogChunk{}, fmt.Errorf("commit log chunk: %w", err)
	}

	return logChunkFromRow(row), nil
}

func (d *Database) SealArtifact(
	ctx context.Context,
	artifactID uuid.UUID,
	sizeBytes int64,
	sha256 string,
	sealedAt time.Time,
) (artifact.Artifact, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("begin artifact seal transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)
	record, err := queries.LockArtifactForLogAppend(ctx, databaseUUID(artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.Artifact{}, artifact.ErrNotFound
	}
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("lock artifact to seal: %w", err)
	}
	if record.State == string(artifact.StateSealed) {
		if record.SizeBytes == sizeBytes && record.Sha256 != nil &&
			*record.Sha256 == sha256 {
			if err := transaction.Commit(ctx); err != nil {
				return artifact.Artifact{}, fmt.Errorf("commit repeated artifact seal: %w", err)
			}

			return artifactFromRow(record), nil
		}

		return artifact.Artifact{}, artifact.ErrSealConflict
	}
	if record.State != string(artifact.StateOpen) || record.SizeBytes != sizeBytes {
		return artifact.Artifact{}, artifact.ErrSealConflict
	}

	sealed, err := queries.SealArtifact(
		ctx,
		storedb.SealArtifactParams{
			SizeBytes: sizeBytes,
			Sha256:    &sha256,
			SealedAt:  databaseTime(sealedAt),
			ID:        databaseUUID(artifactID),
		},
	)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("seal artifact: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return artifact.Artifact{}, fmt.Errorf("commit artifact seal: %w", err)
	}

	return artifactFromRow(sealed), nil
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
