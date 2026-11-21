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
	return d.listGateSummaries(ctx, params, processID, at)
}

func (d *Database) ListPromotionGateSummaries(
	ctx context.Context,
	ids []uuid.UUID,
	processID uuid.UUID,
	at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	params := storedb.ListGateRequirementEvidenceParams{PromotionIds: databaseUUIDs(ids)}
	return d.listGateSummaries(ctx, params, processID, at)
}

func (d *Database) listGateSummaries(
	ctx context.Context,
	params storedb.ListGateRequirementEvidenceParams,
	processID uuid.UUID,
	at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	rows, err := d.queries.ListGateRequirementEvidence(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list Gate requirement evidence: %w", err)
	}

	summaries := make(map[uuid.UUID]gate.Summary)
	for _, row := range rows {
		evidence := gate.RequirementEvidence{
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
		if evidence.ReleasedAt != nil {
			evidence.Observation = gate.Observation{State: gate.ObservationPassing, Value: row.ReleaseObservedValue}
		} else if row.ObservationState != nil {
			evidence.Observation = gate.Observation{
				State:      gate.ObservationState(*row.ObservationState),
				Value:      row.ObservedValue,
				Diagnostic: textValue(row.ObservationDiagnostic),
			}
		}
		id := uuid.UUID(row.SubjectID.Bytes)
		summaries[id] = append(summaries[id], evidence.RequirementAt(processID, at))
	}

	return summaries, nil
}
