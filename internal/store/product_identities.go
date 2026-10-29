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

func (d *Database) CreatePrincipal(
	ctx context.Context,
	record productidentity.Principal,
) (productidentity.Principal, error) {
	row, err := d.queries.CreatePrincipal(
		ctx,
		storedb.CreatePrincipalParams{
			ID:          databaseUUID(record.ID),
			Kind:        string(record.Kind),
			DisplayName: record.DisplayName,
			Role:        string(record.Role),
			CreatedAt:   databaseTime(record.CreatedAt),
			UpdatedAt:   databaseTime(record.UpdatedAt),
		},
	)
	if err != nil {
		return productidentity.Principal{}, fmt.Errorf("insert product principal: %w", err)
	}

	return principalFromRow(row), nil
}

func (d *Database) GetPrincipal(ctx context.Context, principalID uuid.UUID) (productidentity.Principal, error) {
	row, err := d.queries.GetPrincipal(ctx, databaseUUID(principalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.ErrPrincipalNotFound
	}
	if err != nil {
		return productidentity.Principal{}, fmt.Errorf("get product principal: %w", err)
	}

	return principalFromRow(row), nil
}

func (d *Database) ListPrincipals(ctx context.Context) ([]productidentity.Principal, error) {
	rows, err := d.queries.ListPrincipals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list product principals: %w", err)
	}

	principals := make([]productidentity.Principal, 0, len(rows))
	for _, row := range rows {
		principals = append(principals, principalFromRow(row))
	}

	return principals, nil
}

func (d *Database) UpdatePrincipalDisabled(
	ctx context.Context,
	principalID uuid.UUID,
	disabled bool,
	changedAt time.Time,
) (productidentity.Principal, error) {
	row, err := d.queries.UpdatePrincipalDisabled(
		ctx,
		storedb.UpdatePrincipalDisabledParams{
			Disabled:  disabled,
			ChangedAt: databaseTime(changedAt),
			ID:        databaseUUID(principalID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.ErrPrincipalNotFound
	}
	if err != nil {
		return productidentity.Principal{}, fmt.Errorf("update product principal state: %w", err)
	}

	return principalFromRow(row), nil
}

func (d *Database) GetOIDCUser(
	ctx context.Context,
	issuer string,
	subject string,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	row, err := d.queries.GetOIDCIdentity(ctx, storedb.GetOIDCIdentityParams{Issuer: issuer, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityNotFound
	}
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("get OIDC identity: %w", err)
	}

	principal, err := d.GetPrincipal(ctx, uuid.UUID(row.PrincipalID.Bytes))
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{}, err
	}

	return principal, oidcIdentityFromRow(row), nil
}

func (d *Database) CreateOIDCUser(
	ctx context.Context,
	principal productidentity.Principal,
	identity productidentity.OIDCIdentity,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("begin OIDC user transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	principalRow, err := queries.CreatePrincipal(
		ctx,
		storedb.CreatePrincipalParams{
			ID:          databaseUUID(principal.ID),
			Kind:        string(principal.Kind),
			DisplayName: principal.DisplayName,
			Role:        string(principal.Role),
			CreatedAt:   databaseTime(principal.CreatedAt),
			UpdatedAt:   databaseTime(principal.UpdatedAt),
		},
	)
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("insert OIDC principal: %w", err)
	}

	identityRow, err := queries.CreateOIDCIdentity(
		ctx,
		storedb.CreateOIDCIdentityParams{
			ID:                  databaseUUID(identity.ID),
			PrincipalID:         databaseUUID(identity.PrincipalID),
			Issuer:              identity.Issuer,
			Subject:             identity.Subject,
			Email:               identity.Email,
			PreferredUsername:   identity.PreferredUsername,
			DisplayName:         identity.DisplayName,
			LastAuthenticatedAt: databaseTime(identity.LastAuthenticatedAt),
			CreatedAt:           databaseTime(identity.CreatedAt),
			UpdatedAt:           databaseTime(identity.UpdatedAt),
		},
	)
	if identityConflict(err) {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityConflict
	}
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("insert OIDC identity: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("commit OIDC user transaction: %w", err)
	}

	return principalFromRow(principalRow), oidcIdentityFromRow(identityRow), nil
}

func (d *Database) UpdateOIDCUser(
	ctx context.Context,
	principal productidentity.Principal,
	identity productidentity.OIDCIdentity,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("begin OIDC user update: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	principalRow, err := queries.UpdateOIDCPrincipal(
		ctx,
		storedb.UpdateOIDCPrincipalParams{
			DisplayName: principal.DisplayName,
			Role:        string(principal.Role),
			UpdatedAt:   databaseTime(principal.UpdatedAt),
			ID:          databaseUUID(principal.ID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityNotFound
	}
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("update OIDC principal: %w", err)
	}

	identityRow, err := queries.UpdateOIDCIdentity(
		ctx,
		storedb.UpdateOIDCIdentityParams{
			Email:               identity.Email,
			PreferredUsername:   identity.PreferredUsername,
			DisplayName:         identity.DisplayName,
			LastAuthenticatedAt: databaseTime(identity.LastAuthenticatedAt),
			UpdatedAt:           databaseTime(identity.UpdatedAt),
			ID:                  databaseUUID(identity.ID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityNotFound
	}
	if err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("update OIDC identity: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			fmt.Errorf("commit OIDC user update: %w", err)
	}

	return principalFromRow(principalRow), oidcIdentityFromRow(identityRow), nil
}

func principalFromRow(row storedb.Principal) productidentity.Principal {
	return productidentity.Principal{
		ID:          uuid.UUID(row.ID.Bytes),
		Kind:        productidentity.Kind(row.Kind),
		DisplayName: row.DisplayName,
		Role:        productidentity.Role(row.Role),
		DisabledAt:  applicationTime(row.DisabledAt),
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}
}

func oidcIdentityFromRow(row storedb.OidcIdentity) productidentity.OIDCIdentity {
	return productidentity.OIDCIdentity{
		ID:                  uuid.UUID(row.ID.Bytes),
		PrincipalID:         uuid.UUID(row.PrincipalID.Bytes),
		Issuer:              row.Issuer,
		Subject:             row.Subject,
		Email:               row.Email,
		PreferredUsername:   row.PreferredUsername,
		DisplayName:         row.DisplayName,
		LastAuthenticatedAt: row.LastAuthenticatedAt.Time.UTC(),
		CreatedAt:           row.CreatedAt.Time.UTC(),
		UpdatedAt:           row.UpdatedAt.Time.UTC(),
	}
}

func identityConflict(err error) bool {
	return constraintMatches(err, "oidc_identities_issuer_subject_key")
}
