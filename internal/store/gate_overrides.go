package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

// The attachment row owns the logical key. Repository decisions persist the
// exact definition digest alongside the operator's evidence.
type repositoryGateOverride struct {
	DefinitionDigest string `json:"definitionDigest"`
	gate.Decision
}

func (o repositoryGateOverride) project(key gate.Key) gate.Override {
	return gate.Override{Key: key, Version: gate.Version(o.DefinitionDigest), Decision: o.Decision}
}

func (d *Database) RecordGateOverride(ctx context.Context, request gate.OverrideRequest) (gate.Requirement, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if err := request.Validate(); err != nil {
		return gate.Requirement{}, err
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return gate.Requirement{}, fmt.Errorf("begin Gate override: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	queries := d.queries.WithTx(tx)
	id := databaseUUID(request.SubjectID)
	// Serialize with assignment, Promotion release, cancellation and other decisions.
	if request.Subject == gate.RunSubject {
		_, err = queries.GetRunForCancel(ctx, id)
	} else {
		_, err = queries.LockPromotion(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return gate.Requirement{}, gate.ErrSubjectNotFound
	}
	if err != nil {
		return gate.Requirement{}, fmt.Errorf("lock Gate subject: %w", err)
	}

	var requirement gate.Requirement
	if kind, scopeID, server := request.Key.ServerScope(); server {
		requirement, err = recordScopeOverride(ctx, queries, request, kind, scopeID)
	} else {
		requirement, err = recordRepositoryGateOverride(ctx, queries, request)
	}
	if err != nil {
		return gate.Requirement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return gate.Requirement{}, fmt.Errorf("commit Gate override: %w", err)
	}
	return requirement, nil
}

func subjectGateEvidenceParams(subject gate.SubjectType, id pgtype.UUID) storedb.ListGateRequirementEvidenceParams {
	if subject == gate.RunSubject {
		return storedb.ListGateRequirementEvidenceParams{RunIds: []pgtype.UUID{id}}
	}
	return storedb.ListGateRequirementEvidenceParams{PromotionIds: []pgtype.UUID{id}}
}

func recordRepositoryGateOverride(ctx context.Context, queries *storedb.Queries,
	request gate.OverrideRequest) (gate.Requirement, error) {
	id := databaseUUID(request.SubjectID)
	params := subjectGateEvidenceParams(request.Subject, id)
	summaries, err := listRepositoryGateSummaries(ctx, queries, params, request.EvaluatorProcessID, request.RecordedAt)
	if err != nil {
		return gate.Requirement{}, err
	}
	requirements := summaries[request.SubjectID]
	index := slices.IndexFunc(requirements, func(r gate.Requirement) bool { return r.Key == request.Key })
	if index < 0 {
		return gate.Requirement{}, gate.ErrOverrideConflict
	}
	requirement := requirements[index]
	if requirement.Reference != request.Reference || requirement.State == gate.RequirementPassing ||
		requirement.State == gate.RequirementStopped {
		return gate.Requirement{}, gate.ErrOverrideConflict
	}
	if n := len(requirement.Overrides); n > 0 {
		last := requirement.Overrides[n-1]
		if last.Reference == request.Reference {
			if last.Actor.PrincipalID == request.Actor.PrincipalID && last.Reason == request.Reason {
				return requirement, nil
			}
			return gate.Requirement{}, gate.ErrOverrideConflict
		}
	}
	if requirement.State == gate.RequirementReleased {
		return gate.Requirement{}, gate.ErrOverrideConflict
	}
	override := repositoryGateOverride{
		DefinitionDigest: string(request.Version),
		Actor:            request.Actor, Reason: request.Reason, RecordedAt: request.RecordedAt,
		ObservedState: requirement.State, ObservedEvidence: requirement.Evidence,
		EvaluatedAt: requirement.EvaluatedAt, Diagnostic: requirement.Diagnostic,
	}
	decision, err := json.Marshal(override)
	if err != nil {
		return gate.Requirement{}, fmt.Errorf("encode Gate override: %w", err)
	}
	var count int64
	if request.Subject == gate.RunSubject {
		count, err = queries.RecordRunGateOverride(ctx, storedb.RecordRunGateOverrideParams{
			RunID: id, GateName: requirement.Name, DefinitionDigest: override.DefinitionDigest, Decision: decision,
			ProcessID: databaseUUID(request.EvaluatorProcessID), CheckedAt: databaseTime(request.RecordedAt),
		})
	} else {
		count, err = queries.RecordPromotionGateOverride(ctx, storedb.RecordPromotionGateOverrideParams{
			PromotionID: id, GateName: requirement.Name, DefinitionDigest: override.DefinitionDigest, Decision: decision,
			ProcessID: databaseUUID(request.EvaluatorProcessID), CheckedAt: databaseTime(request.RecordedAt),
		})
	}
	if err != nil {
		return gate.Requirement{}, fmt.Errorf("record Gate override: %w", err)
	}
	if count != 1 {
		return gate.Requirement{}, gate.ErrOverrideConflict
	}
	requirement.Overrides = append(requirement.Overrides, override.project(requirement.Key))
	return requirement, nil
}
