package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/run"
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

func (d *Database) ListRunGateProjections(
	ctx context.Context,
	runIDs []uuid.UUID,
) ([]run.GateProjection, error) {
	databaseIDs := make([]pgtype.UUID, 0, len(runIDs))
	for _, runID := range runIDs {
		databaseIDs = append(databaseIDs, databaseUUID(runID))
	}

	rows, err := d.queries.ListRunGateProjections(ctx, databaseIDs)
	if err != nil {
		return nil, fmt.Errorf("list Run Gate projections: %w", err)
	}

	projections := make([]run.GateProjection, 0, len(rows))
	for _, row := range rows {
		var attached catalog.Gate
		if err := json.Unmarshal(row.AttachedDefinition, &attached); err != nil {
			return nil, fmt.Errorf("decode attached Run Gate %q: %w", row.GateName, err)
		}

		projection := run.GateProjection{
			RunID:              uuid.UUID(row.RunID.Bytes),
			Name:               row.GateName,
			AttachedTitle:      attached.Metadata.Title,
			ReleasedAt:         applicationTime(row.ReleasedAt),
			ReleaseValue:       cloneFloat64Pointer(row.ReleaseObservedValue),
			ReleaseEvaluatedAt: applicationTime(row.ReleaseEvaluatedAt),
		}
		if row.ActiveGateDefinition != nil {
			var active catalog.Gate
			if err := json.Unmarshal(row.ActiveGateDefinition, &active); err != nil {
				return nil, fmt.Errorf("decode active Run Gate %q: %w", row.GateName, err)
			}
			projection.ActiveTitle = active.Metadata.Title

			var definition gate.Definition
			if err := json.Unmarshal(row.ActiveNormalizedDefinition, &definition); err != nil {
				return nil, fmt.Errorf("decode active Run Gate definition %q: %w", row.GateName, err)
			}
			projection.ActiveDefinition = &definition
		}
		if row.ObservationState != nil {
			if row.ActiveDefinitionDigest == nil || !row.ActiveCatalogRevisionID.Valid ||
				!row.EvaluatorProcessID.Valid || !row.ObservationEvaluatedAt.Valid ||
				!row.ObservationValidUntil.Valid {
				return nil, fmt.Errorf("run Gate observation %q is incomplete", row.GateName)
			}
			diagnostic := ""
			if row.ObservationDiagnostic != nil {
				diagnostic = *row.ObservationDiagnostic
			}
			projection.CurrentObservation = &gate.CurrentObservation{
				RepositoryID:       uuid.UUID(row.RepositoryID.Bytes),
				GateName:           row.GateName,
				CatalogRevisionID:  uuid.UUID(row.ActiveCatalogRevisionID.Bytes),
				DefinitionDigest:   *row.ActiveDefinitionDigest,
				EvaluatorProcessID: uuid.UUID(row.EvaluatorProcessID.Bytes),
				Observation: gate.Observation{
					State:      gate.ObservationState(*row.ObservationState),
					Value:      cloneFloat64Pointer(row.ObservationValue),
					Diagnostic: diagnostic,
				},
				EvaluatedAt: row.ObservationEvaluatedAt.Time.UTC(),
				ValidUntil:  row.ObservationValidUntil.Time.UTC(),
			}
		}
		if projection.ReleasedAt != nil {
			if row.ReleaseDefinition == nil || projection.ReleaseValue == nil ||
				projection.ReleaseEvaluatedAt == nil {
				return nil, fmt.Errorf("run Gate release evidence %q is incomplete", row.GateName)
			}
			var definition gate.Definition
			if err := json.Unmarshal(row.ReleaseDefinition, &definition); err != nil {
				return nil, fmt.Errorf("decode released Run Gate definition %q: %w", row.GateName, err)
			}
			projection.ReleaseDefinition = &definition
		}

		projections = append(projections, projection)
	}

	return projections, nil
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
