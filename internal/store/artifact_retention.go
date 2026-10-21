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

func (d *Database) ExpireArtifacts(
	ctx context.Context,
	sealedBefore time.Time,
	limit int32,
) ([]artifact.Artifact, error) {
	rows, err := d.queries.ExpireArtifacts(
		ctx,
		storedb.ExpireArtifactsParams{
			SealedBefore: databaseTime(sealedBefore),
			ResultLimit:  limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("expire artifacts: %w", err)
	}

	result := make([]artifact.Artifact, 0, len(rows))
	for _, row := range rows {
		result = append(result, artifactFromRow(row))
	}

	return result, nil
}

func (d *Database) ListExpiredArtifacts(ctx context.Context) ([]artifact.Artifact, error) {
	rows, err := d.queries.ListExpiredArtifacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list expired artifacts: %w", err)
	}

	result := make([]artifact.Artifact, 0, len(rows))
	for _, row := range rows {
		result = append(result, artifactFromRow(row))
	}

	return result, nil
}

func (d *Database) ListArtifactObjectKeys(ctx context.Context, artifactID uuid.UUID) ([]string, error) {
	keys, err := d.queries.ListArtifactObjectKeys(ctx, databaseUUID(artifactID))
	if err != nil {
		return nil, fmt.Errorf("list artifact object keys: %w", err)
	}

	return keys, nil
}

func (d *Database) MarkArtifactDeleted(ctx context.Context, artifactID uuid.UUID) error {
	_, err := d.queries.MarkArtifactDeleted(ctx, databaseUUID(artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("mark artifact deleted: %w", err)
	}

	return nil
}

func (d *Database) ArtifactObjectKeyExists(ctx context.Context, key string) (bool, error) {
	exists, err := d.queries.ArtifactObjectKeyExists(ctx, &key)
	if err != nil {
		return false, fmt.Errorf("check artifact object key: %w", err)
	}

	return exists, nil
}
