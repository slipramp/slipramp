package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	return d.subjectGateSummaries(ctx, gate.RunSubject, params, processID, at)
}

func (d *Database) ListPromotionGateSummaries(
	ctx context.Context, ids []uuid.UUID, processID uuid.UUID, at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	params := storedb.ListGateRequirementEvidenceParams{PromotionIds: databaseUUIDs(ids)}
	return d.subjectGateSummaries(ctx, gate.PromotionSubject, params, processID, at)
}

func (d *Database) subjectGateSummaries(
	ctx context.Context, subject gate.SubjectType, params storedb.ListGateRequirementEvidenceParams,
	processID uuid.UUID, at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	// Release moves all checks from live policy to frozen evidence in one commit.
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin %s Gate summary: %w", subject, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := d.queries.WithTx(tx)
	repository, err := listRepositoryGateSummaries(ctx, q, params, processID, at)
	if err != nil {
		return nil, err
	}
	server, err := listServerGateSummaries(ctx, q, subject, params, at)
	if err != nil {
		return nil, err
	}
	for id, checks := range server {
		repository[id] = append(checks, repository[id]...)
	}
	return repository, tx.Commit(ctx)
}

func listRepositoryGateSummaries(
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
			RepositoryID:       uuid.UUID(row.RepositoryID.Bytes),
			DefinitionDigest:   row.DefinitionDigest,
			Canceled:           row.Canceled,
			Name:               row.GateName,
			Title:              row.Title,
			EvaluatedAt:        applicationTime(row.EvaluatedAt),
			ReleasedAt:         applicationTime(row.ReleasedAt),
			EvaluatorProcessID: uuid.UUID(row.EvaluatorProcessID.Bytes),
			ValidUntil:         row.ValidUntil.Time.UTC(),
		}
		if row.Definition != nil {
			var definition gate.Definition
			if err := json.Unmarshal(row.Definition, &definition); err != nil {
				return nil, fmt.Errorf("decode Gate %q definition: %w", row.GateName, err)
			}
			evidence.Policy = definition.Evaluator
		}
		payload := row.ObservedEvidence
		if evidence.ReleasedAt != nil {
			payload = row.ReleaseEvidence
		} else if row.ObservationState != nil {
			evidence.Observation.State = gate.ObservationState(*row.ObservationState)
			evidence.Observation.Diagnostic = textValue(row.ObservationDiagnostic)
		}
		if payload != nil {
			if err := json.Unmarshal(payload, &evidence.Observation.Evidence); err != nil {
				return nil, fmt.Errorf("decode Gate %q evidence: %w", row.GateName, err)
			}
		}
		id := uuid.UUID(row.SubjectID.Bytes)
		requirement := evidence.RequirementAt(processID, at)
		var decisions []repositoryGateOverride
		if err := json.Unmarshal(row.Overrides, &decisions); err != nil {
			return nil, fmt.Errorf("decode Gate %q overrides: %w", row.GateName, err)
		}
		for _, decision := range decisions {
			requirement.Overrides = append(requirement.Overrides, decision.project(requirement.Key))
		}
		summaries[id] = append(summaries[id], requirement)
	}

	return summaries, nil
}
