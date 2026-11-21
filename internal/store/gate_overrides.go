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

func (d *Database) RecordGateOverride(ctx context.Context, request gate.OverrideRequest) error {
	request.Reason = strings.TrimSpace(request.Reason)
	if err := request.Validate(); err != nil {
		return err
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Gate override: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	queries := d.queries.WithTx(tx)
	id := databaseUUID(request.SubjectID)
	params := storedb.ListGateRequirementEvidenceParams{}
	// Serialize with assignment, Promotion release, cancellation and other decisions.
	if request.Subject == gate.RunSubject {
		_, err = queries.GetRunForCancel(ctx, id)
		params.RunIds = []pgtype.UUID{id}
	} else {
		_, err = queries.LockPromotion(ctx, id)
		params.PromotionIds = []pgtype.UUID{id}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return gate.ErrRequirementNotFound
	}
	if err != nil {
		return fmt.Errorf("lock Gate subject: %w", err)
	}

	summaries, err := listGateSummaries(ctx, queries, params, request.EvaluatorProcessID, request.RecordedAt)
	if err != nil {
		return err
	}
	requirements := summaries[request.SubjectID]
	index := slices.IndexFunc(requirements, func(r gate.Requirement) bool { return r.Name == request.GateName })
	if index < 0 {
		return gate.ErrRequirementNotFound
	}
	requirement := requirements[index]
	if requirement.DefinitionDigest != request.DefinitionDigest {
		return gate.ErrOverrideConflict
	}
	if n := len(requirement.Overrides); n > 0 {
		last := requirement.Overrides[n-1]
		if last.DefinitionDigest == request.DefinitionDigest {
			if last.Actor.PrincipalID == request.Actor.PrincipalID && last.Reason == request.Reason {
				return nil
			}
			return gate.ErrOverrideConflict
		}
	}
	switch requirement.State {
	case gate.RequirementStopped, gate.RequirementReleased, gate.RequirementPassing:
		return gate.ErrOverrideConflict
	}
	decision, err := json.Marshal(gate.Override{
		DefinitionDigest: request.DefinitionDigest, Actor: request.Actor, Reason: request.Reason,
		RecordedAt: request.RecordedAt, ObservedState: requirement.State, ObservedValue: requirement.Value,
		EvaluatedAt: requirement.EvaluatedAt, Diagnostic: requirement.Diagnostic,
	})
	if err != nil {
		return fmt.Errorf("encode Gate override: %w", err)
	}
	var count int64
	if request.Subject == gate.RunSubject {
		count, err = queries.RecordRunGateOverride(ctx, storedb.RecordRunGateOverrideParams{
			RunID: id, GateName: request.GateName, DefinitionDigest: request.DefinitionDigest, Decision: decision,
		})
	} else {
		count, err = queries.RecordPromotionGateOverride(ctx, storedb.RecordPromotionGateOverrideParams{
			PromotionID: id, GateName: request.GateName, DefinitionDigest: request.DefinitionDigest, Decision: decision,
		})
	}
	if err != nil {
		return fmt.Errorf("record Gate override: %w", err)
	}
	if count != 1 {
		return gate.ErrOverrideConflict
	}
	return tx.Commit(ctx)
}
