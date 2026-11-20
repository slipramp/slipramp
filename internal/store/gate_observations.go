package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ListGateEvaluationTargets(ctx context.Context) ([]gate.EvaluationTarget, error) {
	rows, err := d.queries.ListGateEvaluationTargets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Gate evaluation targets: %w", err)
	}

	targets := make([]gate.EvaluationTarget, 0, len(rows))
	for _, row := range rows {
		var definition gate.Definition
		if err := json.Unmarshal(row.NormalizedDefinition, &definition); err != nil {
			return nil, fmt.Errorf("decode Gate evaluation target %q: %w", row.GateName, err)
		}
		targets = append(targets, gate.EvaluationTarget{
			RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
			GateName:          row.GateName,
			CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
			Definition:        definition,
		})
	}

	return targets, nil
}

func (d *Database) UpsertGateObservation(
	ctx context.Context,
	record gate.CurrentObservation,
) error {
	if err := record.Validate(); err != nil {
		return fmt.Errorf("validate Gate observation: %w", err)
	}

	var diagnostic *string
	if record.Observation.Diagnostic != "" {
		diagnostic = &record.Observation.Diagnostic
	}
	err := d.queries.UpsertGateObservation(ctx, storedb.UpsertGateObservationParams{
		RepositoryID:       databaseUUID(record.RepositoryID),
		GateName:           record.GateName,
		CatalogRevisionID:  databaseUUID(record.CatalogRevisionID),
		DefinitionDigest:   record.DefinitionDigest,
		EvaluatorProcessID: databaseUUID(record.EvaluatorProcessID),
		State:              string(record.Observation.State),
		Value:              cloneFloat64Pointer(record.Observation.Value),
		Diagnostic:         diagnostic,
		EvaluatedAt:        databaseTime(record.EvaluatedAt.UTC()),
		ValidUntil:         databaseTime(record.ValidUntil.UTC()),
	})
	if err != nil {
		return fmt.Errorf("upsert Gate observation: %w", err)
	}

	return nil
}

func (d *Database) GetGateObservation(
	ctx context.Context,
	repositoryID uuid.UUID,
	gateName string,
	catalogRevisionID uuid.UUID,
	definitionDigest string,
) (gate.CurrentObservation, error) {
	row, err := d.queries.GetGateObservation(ctx, storedb.GetGateObservationParams{
		RepositoryID:      databaseUUID(repositoryID),
		GateName:          gateName,
		CatalogRevisionID: databaseUUID(catalogRevisionID),
		DefinitionDigest:  definitionDigest,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return gate.CurrentObservation{}, gate.ErrObservationNotFound
	}
	if err != nil {
		return gate.CurrentObservation{}, fmt.Errorf("get Gate observation: %w", err)
	}

	diagnostic := ""
	if row.Diagnostic != nil {
		diagnostic = *row.Diagnostic
	}

	return gate.CurrentObservation{
		RepositoryID:       uuid.UUID(row.RepositoryID.Bytes),
		GateName:           row.GateName,
		CatalogRevisionID:  uuid.UUID(row.CatalogRevisionID.Bytes),
		DefinitionDigest:   row.DefinitionDigest,
		EvaluatorProcessID: uuid.UUID(row.EvaluatorProcessID.Bytes),
		Observation: gate.Observation{
			State:      gate.ObservationState(row.State),
			Value:      cloneFloat64Pointer(row.Value),
			Diagnostic: diagnostic,
		},
		EvaluatedAt: row.EvaluatedAt.Time.UTC(),
		ValidUntil:  row.ValidUntil.Time.UTC(),
	}, nil
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
