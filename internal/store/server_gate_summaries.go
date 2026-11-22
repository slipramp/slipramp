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

func listServerGateSummaries(
	ctx context.Context, q *storedb.Queries, subject gate.SubjectType, params storedb.ListGateRequirementEvidenceParams, at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	rows, err := q.ListWaitingSubjectGateScopes(ctx, storedb.ListWaitingSubjectGateScopesParams{
		RunIds: params.RunIds, PromotionIds: params.PromotionIds, CheckedAt: databaseTime(at),
	})
	if err != nil {
		return nil, fmt.Errorf("list waiting %s Gate scopes: %w", subject, err)
	}
	live := make(map[uuid.UUID][]serverGateScopeSnapshot)
	var waiting []uuid.UUID
	for _, row := range rows {
		id := uuid.UUID(row.SubjectID.Bytes)
		scope, err := serverGateScopeAt(row.ServerGateScope, at)
		if err != nil {
			return nil, err
		}
		if len(live[id]) == 0 {
			waiting = append(waiting, id)
		}
		live[id] = append(live[id], scope)
	}
	history, err := listServerOverridesBySubject(ctx, q, subject, waiting)
	if err != nil {
		return nil, err
	}
	summaries := make(map[uuid.UUID]gate.Summary)
	for id, scopes := range live {
		summaries[id] = serverGateRequirements(scopes, at, history[id])
	}

	releases, err := q.ListSubjectServerGateReleases(ctx, storedb.ListSubjectServerGateReleasesParams(params))
	if err != nil {
		return nil, fmt.Errorf("list %s Gate releases: %w", subject, err)
	}
	for _, row := range releases {
		var release serverGateRelease
		if err := json.Unmarshal(row.ServerGateReleaseEvidence, &release); err != nil {
			return nil, fmt.Errorf("decode %s Gate release: %w", subject, err)
		}
		checks := serverGateRequirements(release.Scopes, release.CheckedAt, nil)
		for i := range checks {
			checks[i].State = gate.RequirementReleased
			checks[i].ReleasedAt = &release.CheckedAt
		}
		summaries[uuid.UUID(row.ID.Bytes)] = checks
	}
	return summaries, nil
}

func serverGateRequirements(scopes []serverGateScopeSnapshot, at time.Time, history []gate.Override) gate.Summary {
	var checks gate.Summary
	for _, kind := range []string{gate.LockType, gate.WindowType} {
		for _, scope := range scopes {
			if check := scope.requirement(kind, at); check.Key != "" {
				checks = append(checks, withServerGateOverrideHistory(check, history))
			}
		}
	}
	return checks
}

func withServerGateOverrideHistory(check gate.Requirement, history []gate.Override) gate.Requirement {
	for _, decision := range history {
		if decision.Key == check.Key {
			check.Overrides = append(check.Overrides, decision)
		}
	}
	return check
}
