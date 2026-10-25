package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const (
	artifactLogChunksPrimaryKeyConstraint = "artifact_log_chunks_pkey"
	artifactLogChunksForeignKeyConstraint = "artifact_log_chunks_artifact_id_fkey"
	artifactLogChunksObjectKeyConstraint  = "artifact_log_chunks_object_key_key"
)

func (d *Database) CreateLogChunk(ctx context.Context, chunk artifact.LogChunk) (artifact.LogChunk, error) {
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

	record, err := queries.LockArtifactForLogAppend(ctx, databaseUUID(chunk.ArtifactID))
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
		if constraintMatches(err, artifactLogChunksPrimaryKeyConstraint, artifactLogChunksObjectKeyConstraint) {
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

func (d *Database) FailArtifact(ctx context.Context, artifactID uuid.UUID) (artifact.Artifact, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("begin artifact failure transaction: %w", err)
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
		return artifact.Artifact{}, fmt.Errorf("lock artifact to fail: %w", err)
	}
	if record.State == string(artifact.StateSealed) {
		return artifact.Artifact{}, artifact.ErrSealConflict
	}
	if record.State == string(artifact.StateFailed) {
		if err := transaction.Commit(ctx); err != nil {
			return artifact.Artifact{}, fmt.Errorf("commit repeated artifact failure: %w", err)
		}

		return artifactFromRow(record), nil
	}
	if record.State != string(artifact.StateOpen) {
		return artifact.Artifact{}, artifact.ErrSealConflict
	}

	failed, err := queries.FailArtifact(ctx, databaseUUID(artifactID))
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("fail artifact: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return artifact.Artifact{}, fmt.Errorf("commit artifact failure: %w", err)
	}

	return artifactFromRow(failed), nil
}

func (d *Database) GetLogChunk(
	ctx context.Context,
	artifactID uuid.UUID,
	offsetBytes int64,
) (artifact.LogChunk, error) {
	if offsetBytes < 0 {
		return artifact.LogChunk{}, fmt.Errorf("%w: chunk offset must not be negative", artifact.ErrInvalid)
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
		return nil, fmt.Errorf("%w: starting chunk offset must not be negative", artifact.ErrInvalid)
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
