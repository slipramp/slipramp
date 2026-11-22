package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

type gateCheckClock func(*storedb.Queries, context.Context) (pgtype.Timestamptz, error)

const windowUnavailableDiagnostic = "The weekly window observation is outside its validity interval."

// Each release freezes both scope versions, including scopes without active policy.
// At most four decisions can be selected, one for each applicable policy.
type serverGateRelease struct {
	CheckedAt time.Time                 `json:"checkedAt"`
	Scopes    []serverGateScopeSnapshot `json:"scopes"`
}

type serverGateScopeSnapshot struct {
	ID             uuid.UUID         `json:"id"`
	Kind           gate.ScopeKind    `json:"kind"`
	LockVersion    int64             `json:"lockVersion"`
	WindowVersion  int64             `json:"windowVersion"`
	Lock           *gate.ScopeLock   `json:"lock,omitempty"`
	Window         *gate.ScopeWindow `json:"window,omitempty"`
	LockOverride   *gate.Decision    `json:"lockOverride,omitempty"`
	WindowOverride *gate.Decision    `json:"windowOverride,omitempty"`
}

func serverGateScopeAt(row storedb.ServerGateScope, at time.Time) (serverGateScopeSnapshot, error) {
	scope := serverGateScopeSnapshot{
		ID: uuid.UUID(row.ID.Bytes), Kind: gate.ScopeKind(row.Kind),
		LockVersion: row.LockVersion, WindowVersion: row.WindowVersion,
	}
	if serverGateLockActive(row, at) {
		lock, err := scopeLockFromRow(row)
		if err != nil {
			return scope, err
		}
		scope.Lock = lock
	}
	window, err := windowFromRow(row, at)
	scope.Window = window
	return scope, err
}

func (scope serverGateScopeSnapshot) requirement(kind string, at time.Time) gate.Requirement {
	titles := map[gate.ScopeKind]string{gate.GlobalScope: "Global", gate.TargetScope: "Target", gate.StageScope: "Stage"}
	check := gate.Requirement{
		Origin: gate.ServerOrigin, Type: kind, Scope: gate.Scope{Kind: scope.Kind, ID: scope.ID},
		Name: string(scope.Kind) + "-" + kind, Title: titles[scope.Kind] + " " + kind,
	}
	var decision *gate.Decision
	switch kind {
	case gate.LockType:
		if scope.Lock == nil {
			return gate.Requirement{}
		}
		check.Reference = serverGateReference(kind, scope.ID, scope.LockVersion)
		check.State, check.EvaluatedAt, check.ValidUntil = gate.RequirementBlocking, &at, scope.Lock.ExpiresAt
		check.Evidence = &gate.Evidence{Payload: scope.Lock}
		decision = scope.LockOverride
	case gate.WindowType:
		w := scope.Window
		if w == nil {
			return gate.Requirement{}
		}
		check.Reference = serverGateReference(kind, scope.ID, scope.WindowVersion)
		check.State, check.EvaluatedAt, check.ValidUntil = w.State, &w.Observation.EvaluatedAt, &w.Observation.ValidUntil
		check.Evidence = &gate.Evidence{Payload: &gate.WindowEvidence{TimeZone: w.Definition.TimeZone, Digest: w.Digest, Observation: w.Observation}}
		if w.State == gate.RequirementUnavailable {
			check.Diagnostic = windowUnavailableDiagnostic
		}
		decision = scope.WindowOverride
	}
	if decision != nil {
		check.Overrides = []gate.Override{{Reference: check.Reference, Decision: *decision}}
	}
	return check
}

func serverGateReference(kind string, scopeID uuid.UUID, version int64) gate.Reference {
	return gate.Reference{Key: gate.Key(kind + "/" + scopeID.String()), Version: gate.Version(strconv.FormatInt(version, 10))}
}

// A nil snapshot means that a policy is blocking release. Both scope rows are locked by the caller.
func freezeServerGates(
	ctx context.Context, q *storedb.Queries, subject gate.SubjectType, id uuid.UUID, rows []storedb.ServerGateScope, at time.Time,
) ([]byte, error) {
	local := gate.TargetScope
	if subject == gate.PromotionSubject {
		local = gate.StageScope
	}
	if len(rows) != 2 || rows[0].Kind != string(gate.GlobalScope) || rows[1].Kind != string(local) {
		return nil, fmt.Errorf("missing required Gate scopes for %s %s", subject, id)
	}
	release := serverGateRelease{CheckedAt: at}
	for _, row := range rows {
		scope, err := serverGateScopeAt(row, at)
		if err != nil {
			return nil, err
		}
		release.Scopes = append(release.Scopes, scope)
	}
	if release.Scopes[0].Lock == nil && release.Scopes[0].Window == nil &&
		release.Scopes[1].Lock == nil && release.Scopes[1].Window == nil {
		return json.Marshal(release)
	}

	history, err := listSubjectServerOverrides(ctx, q, subject, id)
	if err != nil {
		return nil, err
	}
	decisions := make(map[gate.Reference]*gate.Decision, len(history))
	for i := range history {
		decisions[history[i].Reference] = &history[i].Decision
	}
	for i := range release.Scopes {
		scope := &release.Scopes[i]
		if scope.Lock != nil {
			scope.LockOverride = decisions[serverGateReference(gate.LockType, scope.ID, scope.LockVersion)]
			if scope.LockOverride == nil {
				return nil, nil
			}
		}
		if scope.Window != nil {
			scope.WindowOverride = decisions[serverGateReference(gate.WindowType, scope.ID, scope.WindowVersion)]
			if scope.WindowOverride == nil && scope.Window.State != gate.RequirementPassing {
				return nil, nil
			}
		}
	}
	return json.Marshal(release)
}
