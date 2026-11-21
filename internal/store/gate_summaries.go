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

func (d *Database) ListRunGateSummaries(
	ctx context.Context,
	ids []uuid.UUID,
	processID uuid.UUID,
	at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	params := storedb.ListGateRequirementEvidenceParams{RunIds: databaseUUIDs(ids)}
	return listGateSummaries(ctx, d.queries, params, processID, at)
}

func (d *Database) ListPromotionGateSummaries(
	ctx context.Context,
	ids []uuid.UUID,
	processID uuid.UUID,
	at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	params := storedb.ListGateRequirementEvidenceParams{PromotionIds: databaseUUIDs(ids)}
	return listGateSummaries(ctx, d.queries, params, processID, at)
}

func listGateSummaries(
	ctx context.Context,
	queries *storedb.Queries,
	params storedb.ListGateRequirementEvidenceParams,
	processID uuid.UUID,
	at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	rows, err := queries.ListGateRequirementEvidence(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list Gate requirement evidence: %w", err)
	}

	summaries := make(map[uuid.UUID]gate.Summary)
	for _, row := range rows {
		evidence := gate.RequirementEvidence{
			Canceled:           row.Canceled,
			Name:               row.GateName,
			Title:              row.Title,
			EvaluatedAt:        applicationTime(row.EvaluatedAt),
			ReleasedAt:         applicationTime(row.ReleasedAt),
			EvaluatorProcessID: uuid.UUID(row.EvaluatorProcessID.Bytes),
			ValidUntil:         row.ValidUntil.Time.UTC(),
		}
		if row.Condition != nil {
			var condition gate.Condition
			if err := json.Unmarshal(row.Condition, &condition); err != nil {
				return nil, fmt.Errorf("decode Gate %q condition: %w", row.GateName, err)
			}
			evidence.Condition = &condition
		}
		if evidence.ReleasedAt != nil && row.ReleaseObservedValue != nil {
			evidence.Observation = gate.Observation{State: gate.ObservationPassing, Value: row.ReleaseObservedValue}
		} else if row.ObservationState != nil {
			evidence.Observation = gate.Observation{
				State:      gate.ObservationState(*row.ObservationState),
				Value:      row.ObservedValue,
				Diagnostic: textValue(row.ObservationDiagnostic),
			}
		}
		id := uuid.UUID(row.SubjectID.Bytes)
		requirement := evidence.RequirementAt(processID, at)
		requirement.DefinitionDigest = row.DefinitionDigest
		if err := json.Unmarshal(row.Overrides, &requirement.Overrides); err != nil {
			return nil, fmt.Errorf("decode Gate %q overrides: %w", row.GateName, err)
		}
		summaries[id] = append(summaries[id], requirement)
	}

	return summaries, nil
}
