package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ListGateEvaluationDemand(ctx context.Context) ([]gate.EvaluationDemand, error) {
	rows, err := d.queries.ListGateEvaluationDemand(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Gate evaluation demand: %w", err)
	}

	demand := make([]gate.EvaluationDemand, 0, len(rows))
	for _, row := range rows {
		var definition gate.Definition
		if err := json.Unmarshal(row.Spec, &definition); err != nil {
			return nil, fmt.Errorf("decode Gate definition for %q: %w", row.GateName, err)
		}
		demand = append(demand, gate.EvaluationDemand{
			RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
			GateName:          row.GateName,
			CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
			Definition:        definition,
			DefinitionDigest:  row.DefinitionDigest,
			WaiterCount:       row.WaiterCount,
			LastRequestedAt:   row.LastRequestedAt.Time.UTC(),
		})
	}

	return demand, nil
}

func (d *Database) UpsertGateObservation(
	ctx context.Context,
	record gate.CurrentObservation,
) error {
	if err := record.Validate(); err != nil {
		return fmt.Errorf("validate Gate observation: %w", err)
	}

	err := d.queries.UpsertGateObservation(ctx, storedb.UpsertGateObservationParams{
		RepositoryID:       databaseUUID(record.RepositoryID),
		GateName:           record.GateName,
		CatalogRevisionID:  databaseUUID(record.CatalogRevisionID),
		DefinitionDigest:   record.DefinitionDigest,
		EvaluatorProcessID: databaseUUID(record.EvaluatorProcessID),
		State:              string(record.Observation.State),
		Value:              record.Observation.Value,
		Diagnostic:         nullableText(record.Observation.Diagnostic),
		EvaluatedAt:        databaseTime(record.EvaluatedAt.UTC()),
		ValidUntil:         databaseTime(record.ValidUntil.UTC()),
	})
	if err != nil {
		return fmt.Errorf("upsert Gate observation: %w", err)
	}

	return nil
}

func (d *Database) DeleteInactiveGateObservations(ctx context.Context, inactiveBefore time.Time) (int64, error) {
	deleted, err := d.queries.DeleteInactiveGateObservations(ctx, databaseTime(inactiveBefore))
	if err != nil {
		return 0, fmt.Errorf("delete inactive Gate observations: %w", err)
	}

	return deleted, nil
}
