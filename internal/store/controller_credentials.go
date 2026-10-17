package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ controlleridentity.Store = (*Database)(nil)

func (d *Database) EnrollControllerCredential(
	ctx context.Context,
	credential controlleridentity.Credential,
) error {
	if err := credential.Validate(); err != nil {
		return err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin controller enrollment transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	existing, err := queries.GetControllerCredentialForUpdate(
		ctx,
		credential.ClusterName,
	)
	credentialMissing := errors.Is(err, pgx.ErrNoRows)
	if err == nil && existing.Sha256 != credential.SHA256 {
		return controlleridentity.ErrEnrollmentConflict
	}
	if err != nil && !credentialMissing {
		return fmt.Errorf("get controller enrollment: %w", err)
	}

	if err := queries.RecordClusterObservation(
		ctx,
		storedb.RecordClusterObservationParams{
			Name:       credential.ClusterName,
			ObservedAt: databaseTime(credential.EnrolledAt),
		},
	); err != nil {
		return fmt.Errorf("record enrolled cluster: %w", err)
	}

	if credentialMissing {
		rowsAffected, createErr := queries.CreateControllerCredential(
			ctx,
			storedb.CreateControllerCredentialParams{
				ClusterName: credential.ClusterName,
				Sha256:      credential.SHA256,
				EnrolledAt:  databaseTime(credential.EnrolledAt),
			},
		)
		if createErr != nil {
			return fmt.Errorf("create controller credential: %w", createErr)
		}

		if rowsAffected == 0 {
			existing, getErr := queries.GetControllerCredentialForUpdate(
				ctx,
				credential.ClusterName,
			)
			if errors.Is(getErr, pgx.ErrNoRows) {
				return controlleridentity.ErrEnrollmentConflict
			}
			if getErr != nil {
				return fmt.Errorf("get concurrent controller enrollment: %w", getErr)
			}
			if existing.Sha256 != credential.SHA256 {
				return controlleridentity.ErrEnrollmentConflict
			}
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit controller enrollment: %w", err)
	}

	return nil
}

func (d *Database) GetControllerClusterByCredentialSHA256(
	ctx context.Context,
	credentialSHA256 string,
) (string, error) {
	clusterName, err := d.queries.GetControllerClusterByCredentialSHA256(
		ctx,
		credentialSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", controlleridentity.ErrCredentialNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get controller credential: %w", err)
	}

	return clusterName, nil
}
