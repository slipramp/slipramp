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
		var subject *gate.EvaluationSubject
		if row.Subject != nil {
			if err := json.Unmarshal(row.Subject, &subject); err != nil {
				return nil, fmt.Errorf("decode Gate evaluation subject: %w", err)
			}
		}
		demand = append(demand, gate.EvaluationDemand{
			RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
			Subject:           subject,
			RepositoryName:    row.RepositoryName,
			CommitSHA:         row.CommitSha,
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

	var payload []byte
	if record.Observation.Evidence != nil {
		var err error
		payload, err = json.Marshal(record.Observation.Evidence)
		if err != nil {
			return fmt.Errorf("encode Gate observation: %w", err)
		}
	}
	shared := storedb.UpsertGateObservationParams{
		RepositoryID:       databaseUUID(record.RepositoryID),
		GateName:           record.GateName,
		CatalogRevisionID:  databaseUUID(record.CatalogRevisionID),
		DefinitionDigest:   record.DefinitionDigest,
		EvaluatorProcessID: databaseUUID(record.EvaluatorProcessID),
		State:              string(record.Observation.State),
		Evidence:           payload,
		Diagnostic:         nullableText(record.Observation.Diagnostic),
		EvaluatedAt:        databaseTime(record.EvaluatedAt.UTC()),
		ValidUntil:         databaseTime(record.ValidUntil.UTC()),
	}
	var err error
	if record.SubjectID == uuid.Nil {
		err = d.queries.UpsertGateObservation(ctx, shared)
	} else {
		subject := storedb.RecordRunGateObservationParams{
			SubjectID:          databaseUUID(record.SubjectID),
			RepositoryID:       shared.RepositoryID,
			GateName:           shared.GateName,
			CatalogRevisionID:  shared.CatalogRevisionID,
			DefinitionDigest:   &shared.DefinitionDigest,
			EvaluatorProcessID: shared.EvaluatorProcessID,
			State:              &shared.State,
			Evidence:           payload,
			Diagnostic:         shared.Diagnostic,
			EvaluatedAt:        shared.EvaluatedAt,
			ValidUntil:         shared.ValidUntil,
		}
		if record.SubjectType == gate.RunSubject {
			err = d.queries.RecordRunGateObservation(ctx, subject)
		} else {
			err = d.queries.RecordPromotionGateObservation(ctx, storedb.RecordPromotionGateObservationParams(subject))
		}
	}

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
