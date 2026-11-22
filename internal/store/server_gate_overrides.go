package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func gateOverrideSubject(ctx context.Context, queries *storedb.Queries, subject gate.SubjectType,
	id uuid.UUID) (storedb.GetGateOverrideSubjectRow, error) {
	row, err := queries.GetGateOverrideSubject(ctx, storedb.GetGateOverrideSubjectParams{
		SubjectType: string(subject), SubjectID: databaseUUID(id),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, gate.ErrSubjectNotFound
	}
	if err != nil {
		return row, fmt.Errorf("read Gate override subject: %w", err)
	}
	return row, nil
}

// The caller holds the subject row through commit. Scope mutations take the same
// global and local scope locks. Sample expiry only after acquiring those locks.
func recordScopeOverride(ctx context.Context, queries *storedb.Queries, request gate.OverrideRequest,
	kind string, scopeID uuid.UUID) (gate.Requirement, error) {
	subject, err := gateOverrideSubject(ctx, queries, request.Subject, request.SubjectID)
	if err != nil {
		return gate.Requirement{}, err
	}
	if !subject.Waiting || !subject.UsesServerGates {
		return gate.Requirement{}, gate.ErrOverrideConflict
	}
	scopes, err := queries.LockServerGateScopes(ctx, storedb.LockServerGateScopesParams{
		Kind: subject.Kind, OwnerID: subject.OwnerID,
	})
	if err != nil {
		return gate.Requirement{}, fmt.Errorf("lock Server Gate scopes for override: %w", err)
	}
	at, err := queries.ServerGateCheckTime(ctx)
	if err != nil {
		return gate.Requirement{}, fmt.Errorf("read Gate override time: %w", err)
	}
	index := slices.IndexFunc(scopes, func(scope storedb.ServerGateScope) bool { return scope.ID == databaseUUID(scopeID) })
	if index < 0 {
		return gate.Requirement{}, gate.ErrOverrideConflict
	}
	row := scopes[index]
	scope, err := serverGateScopeAt(row, at.Time.UTC())
	if err != nil {
		return gate.Requirement{}, err
	}
	history, err := listSubjectServerOverrides(ctx, queries, request.Subject, request.SubjectID)
	if err != nil {
		return gate.Requirement{}, err
	}
	check := withServerGateOverrideHistory(scope.requirement(kind, at.Time.UTC()), history)
	if check.Reference != request.Reference || check.State == gate.RequirementPassing {
		return gate.Requirement{}, gate.ErrOverrideConflict
	}
	if n := len(check.Overrides); n > 0 && check.Overrides[n-1].Reference == check.Reference {
		last := check.Overrides[n-1]
		if last.Actor.PrincipalID == request.Actor.PrincipalID && last.Reason == request.Reason {
			return check, nil
		}
		return gate.Requirement{}, gate.ErrOverrideConflict
	}
	actorID, actor, err := actorDatabaseFields(&request.Actor)
	if err != nil {
		return gate.Requirement{}, err
	}
	args := storedb.InsertServerGateOverrideParams{
		SubjectType: string(request.Subject), SubjectID: databaseUUID(request.SubjectID),
		ScopeID: row.ID, Version: scope.LockVersion,
		GateType: kind, ObservedState: string(check.State),
		ActorID: actorID, Actor: actor, Reason: request.Reason, RecordedAt: at,
	}
	if kind == gate.WindowType {
		args.Version = scope.WindowVersion
		args.WindowObservation, err = json.Marshal(check.Evidence.Payload.(*gate.WindowEvidence).Observation)
		if err != nil {
			return gate.Requirement{}, err
		}
	}
	if err := queries.InsertServerGateOverride(ctx, args); err != nil {
		return gate.Requirement{}, fmt.Errorf("record Server Gate override: %w", err)
	}
	check.Overrides = append(check.Overrides, gate.Override{
		Reference: check.Reference, Actor: request.Actor, Reason: request.Reason, RecordedAt: at.Time.UTC(),
		ObservedState: check.State, ObservedEvidence: check.Evidence, EvaluatedAt: check.EvaluatedAt, Diagnostic: check.Diagnostic,
	})
	return check, nil
}

func listSubjectServerOverrides(ctx context.Context, queries *storedb.Queries, subject gate.SubjectType,
	id uuid.UUID) ([]gate.Override, error) {
	bySubject, err := listServerOverridesBySubject(ctx, queries, subject, []uuid.UUID{id})
	return bySubject[id], err
}

func listServerOverridesBySubject(ctx context.Context, queries *storedb.Queries, subject gate.SubjectType,
	ids []uuid.UUID) (map[uuid.UUID][]gate.Override, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := queries.ListSubjectServerGateOverrides(ctx, storedb.ListSubjectServerGateOverridesParams{
		SubjectType: string(subject), SubjectIds: databaseUUIDs(ids),
	})
	if err != nil {
		return nil, fmt.Errorf("read Server Gate override history: %w", err)
	}
	decisions := make(map[uuid.UUID][]gate.Override)
	for _, row := range rows {
		actor, err := actorFromDatabase(row.ActorID, row.Actor)
		if err != nil {
			return nil, err
		}
		decision := gate.Override{
			Reference: serverGateReference(row.GateType, uuid.UUID(row.ScopeID.Bytes), row.Version),
			Actor:     *actor, Reason: row.Reason, RecordedAt: row.RecordedAt.Time.UTC(),
			ObservedState: gate.RequirementState(row.ObservedState),
		}
		if row.GateType == gate.WindowType {
			var o gate.WindowObservation
			if err := json.Unmarshal(row.WindowObservation, &o); err != nil {
				return nil, err
			}
			decision.EvaluatedAt = &o.EvaluatedAt
			decision.ObservedEvidence = &gate.Evidence{Payload: &gate.WindowEvidence{TimeZone: row.TimeZone, Digest: *row.WindowDigest, Observation: o}}
			if decision.ObservedState == gate.RequirementUnavailable {
				decision.Diagnostic = windowUnavailableDiagnostic
			}
		} else {
			lockActor, err := actorFromDatabase(row.LockActorID, row.LockActor)
			if err != nil {
				return nil, err
			}
			decision.EvaluatedAt = applicationTime(row.RecordedAt)
			decision.ObservedEvidence = &gate.Evidence{Payload: &gate.ScopeLock{Actor: *lockActor, Reason: row.LockReason,
				CreatedAt: row.LockedAt.Time.UTC(), ExpiresAt: applicationTime(row.ExpiresAt)}}
		}
		id := uuid.UUID(row.SubjectID.Bytes)
		decisions[id] = append(decisions[id], decision)
	}
	return decisions, nil
}

// Read subject eligibility, current policy and immutable decisions from one snapshot.
func (d *Database) ListGateOverrides(
	ctx context.Context, kind gate.SubjectType, id, processID uuid.UUID,
) (gate.OverrideCollection, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return gate.OverrideCollection{}, fmt.Errorf("begin Gate override read: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	queries := d.queries.WithTx(tx)
	subject, err := gateOverrideSubject(ctx, queries, kind, id)
	if err != nil {
		return gate.OverrideCollection{}, err
	}
	history, err := listSubjectServerOverrides(ctx, queries, kind, id)
	if err != nil {
		return gate.OverrideCollection{}, err
	}
	result := gate.OverrideCollection{Decisions: history}
	at := subject.CheckedAt.Time.UTC()
	if subject.Waiting && subject.UsesServerGates {
		rows, err := queries.ListApplicableServerGateScopes(ctx, storedb.ListApplicableServerGateScopesParams{
			Kind: subject.Kind, OwnerID: subject.OwnerID,
		})
		if err != nil {
			return result, fmt.Errorf("read applicable Server Gate scopes: %w", err)
		}
		scopes := make([]serverGateScopeSnapshot, 0, len(rows))
		for _, row := range rows {
			scope, err := serverGateScopeAt(row, at)
			if err != nil {
				return result, err
			}
			scopes = append(scopes, scope)
		}
		for _, check := range serverGateRequirements(scopes, at, history) {
			if eligibleForNewOverride(check) {
				result.Eligible = append(result.Eligible, check)
			}
		}
	}
	params := subjectGateEvidenceParams(kind, databaseUUID(id))
	repository, err := listRepositoryGateSummaries(ctx, queries, params, processID, at)
	if err != nil {
		return result, err
	}
	for _, check := range repository[id] {
		result.Decisions = append(result.Decisions, check.Overrides...)
		if subject.Waiting && eligibleForNewOverride(check) {
			result.Eligible = append(result.Eligible, check)
		}
	}
	slices.SortStableFunc(result.Decisions, func(a, b gate.Override) int {
		if order := a.RecordedAt.Compare(b.RecordedAt); order != 0 {
			return order
		}
		return strings.Compare(string(a.Key), string(b.Key))
	})
	return result, nil
}

func eligibleForNewOverride(check gate.Requirement) bool {
	if check.Version == "" {
		return false
	}
	if n := len(check.Overrides); n > 0 && check.Overrides[n-1].Reference == check.Reference {
		return false
	}
	switch check.State {
	case gate.RequirementBlocking, gate.RequirementUnavailable, gate.RequirementUnevaluated:
		return true
	default:
		return false
	}
}
