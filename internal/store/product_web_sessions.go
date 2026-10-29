package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

const webSessionTokenDigestConstraint = "web_sessions_token_digest_key"

func (d *Database) CreateWebSession(
	ctx context.Context,
	record productidentity.WebSession,
) (productidentity.WebSession, error) {
	row, err := d.queries.CreateWebSession(
		ctx,
		storedb.CreateWebSessionParams{
			ID:          databaseUUID(record.ID),
			PrincipalID: databaseUUID(record.PrincipalID),
			TokenDigest: record.TokenDigest[:],
			CreatedAt:   databaseTime(record.CreatedAt),
			ExpiresAt:   databaseTime(record.ExpiresAt),
			LastSeenAt:  databaseTime(record.LastSeenAt),
		},
	)
	if constraintMatches(err, webSessionTokenDigestConstraint) {
		return productidentity.WebSession{}, productidentity.ErrCredentialConflict
	}
	if err != nil {
		return productidentity.WebSession{}, fmt.Errorf("insert web session: %w", err)
	}

	return webSessionFromRow(row)
}

func (d *Database) GetWebSessionPrincipal(
	ctx context.Context,
	tokenDigest productidentity.CredentialDigest,
) (productidentity.WebSession, productidentity.Principal, error) {
	row, err := d.queries.GetWebSessionByDigest(ctx, tokenDigest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.WebSession{}, productidentity.Principal{},
			productidentity.ErrCredentialNotFound
	}
	if err != nil {
		return productidentity.WebSession{}, productidentity.Principal{},
			fmt.Errorf("get web session: %w", err)
	}

	session, err := webSessionFromRow(row)
	if err != nil {
		return productidentity.WebSession{}, productidentity.Principal{}, err
	}
	principal, err := d.GetPrincipal(ctx, session.PrincipalID)
	if err != nil {
		return productidentity.WebSession{}, productidentity.Principal{}, err
	}

	return session, principal, nil
}

func (d *Database) TouchWebSession(ctx context.Context, sessionID uuid.UUID, lastSeenAt time.Time) error {
	rowsAffected, err := d.queries.TouchWebSession(
		ctx,
		storedb.TouchWebSessionParams{
			LastSeenAt: databaseTime(lastSeenAt),
			ID:         databaseUUID(sessionID),
		},
	)
	if err != nil {
		return fmt.Errorf("touch web session: %w", err)
	}
	if rowsAffected != 1 {
		return productidentity.ErrCredentialNotFound
	}

	return nil
}

func (d *Database) DeleteWebSession(ctx context.Context, sessionID uuid.UUID) error {
	rowsAffected, err := d.queries.DeleteWebSession(ctx, databaseUUID(sessionID))
	if err != nil {
		return fmt.Errorf("delete web session: %w", err)
	}
	if rowsAffected != 1 {
		return productidentity.ErrCredentialNotFound
	}

	return nil
}

func (d *Database) DeleteWebSessionsByPrincipal(ctx context.Context, principalID uuid.UUID) error {
	if _, err := d.queries.DeleteWebSessionsByPrincipal(ctx, databaseUUID(principalID)); err != nil {
		return fmt.Errorf("delete principal web sessions: %w", err)
	}

	return nil
}

func webSessionFromRow(row storedb.WebSession) (productidentity.WebSession, error) {
	digest, err := credentialDigest(row.TokenDigest)
	if err != nil {
		return productidentity.WebSession{}, fmt.Errorf("decode web session digest: %w", err)
	}

	return productidentity.WebSession{
		ID:          uuid.UUID(row.ID.Bytes),
		PrincipalID: uuid.UUID(row.PrincipalID.Bytes),
		TokenDigest: digest,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		ExpiresAt:   row.ExpiresAt.Time.UTC(),
		LastSeenAt:  row.LastSeenAt.Time.UTC(),
	}, nil
}
