package api

import (
	"strconv"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func NewActorSnapshot(value productidentity.ActorSnapshot) ActorSnapshot {
	return ActorSnapshot{
		PrincipalID: value.PrincipalID,
		Kind:        ActorSnapshotKind(value.Kind),
		DisplayName: value.DisplayName,
	}
}

func NewServerGateScope(state gate.ScopeState) ServerGateScope {
	result := ServerGateScope{
		ID: state.ID, Kind: ServerGateScopeKind(state.Owner.Kind), CheckedAt: state.CheckedAt,
		LockVersion: strconv.FormatInt(state.LockVersion, 10),
		Window:      state.Window, WindowVersion: strconv.FormatInt(state.WindowVersion, 10),
		WindowEvents: make([]ServerWindowEvent, 0, len(state.WindowEvents)),
		Events:       make([]ServerGateLockEvent, 0, len(state.Events)),
	}
	switch state.Owner.Kind {
	case gate.TargetScope:
		result.TargetID = &state.Owner.ID
	case gate.StageScope:
		result.StageID = &state.Owner.ID
	}
	if lock := state.Lock; lock != nil {
		result.Lock = &ServerGateLock{
			Actor: NewActorSnapshot(lock.Actor), Reason: lock.Reason, CreatedAt: lock.CreatedAt, ExpiresAt: lock.ExpiresAt,
		}
	}
	for _, event := range state.Events {
		result.Events = append(result.Events, ServerGateLockEvent{
			Action: ServerGateLockEventAction(event.Action), Actor: NewActorSnapshot(event.Actor),
			Reason: event.Reason, RecordedAt: event.RecordedAt, ExpiresAt: event.ExpiresAt,
			PriorVersion: strconv.FormatInt(event.Version-1, 10), Version: strconv.FormatInt(event.Version, 10),
		})
	}
	for _, event := range state.WindowEvents {
		result.WindowEvents = append(result.WindowEvents, ServerWindowEvent{
			Action: ServerWindowEventAction(event.Action), Version: strconv.FormatInt(event.Version, 10),
			PriorVersion: strconv.FormatInt(event.Version-1, 10), Actor: NewActorSnapshot(event.Actor),
			RecordedAt: event.RecordedAt, Definition: event.Definition, Digest: event.Digest,
		})
	}
	return result
}

func NewGateRequirement(requirement gate.Requirement, sources gate.SourceSet) GateRequirement {
	var operator *GateOperator
	var threshold *float64
	if definition, ok := requirement.Policy.(*gate.PrometheusDefinition); ok {
		operator = new(GateOperator(definition.Condition.Operator))
		threshold = &definition.Condition.Value
	}
	var overrides *[]GateOverride
	if len(requirement.Overrides) > 0 {
		overrides = new(gateOverrideResponses(requirement.Overrides))
	}
	return GateRequirement{
		Name:        CatalogName(requirement.Name),
		Key:         string(requirement.Key),
		Version:     optionalString(string(requirement.Version)),
		Type:        optionalString(requirement.Type),
		Origin:      GateRequirementOrigin(requirement.Origin),
		Scope:       GateScope{Kind: GateScopeKind(requirement.Scope.Kind), ID: requirement.Scope.ID},
		Overrides:   overrides,
		QueryURL:    sources.QueryURL(requirement),
		Title:       optionalString(requirement.Title),
		State:       GateRequirementState(requirement.State),
		Evidence:    requirement.Evidence,
		Operator:    operator,
		Threshold:   threshold,
		EvaluatedAt: requirement.EvaluatedAt,
		ValidUntil:  requirement.ValidUntil,
		ReleasedAt:  requirement.ReleasedAt,
		Diagnostic:  optionalString(requirement.Diagnostic),
	}
}

func gateOverrideResponses(decisions []gate.Override) []GateOverride {
	result := make([]GateOverride, 0, len(decisions))
	for _, decision := range decisions {
		result = append(result, GateOverride{
			Key:              string(decision.Key),
			Version:          string(decision.Version),
			Actor:            NewActorSnapshot(decision.Actor),
			Reason:           decision.Reason,
			RecordedAt:       decision.RecordedAt,
			ObservedState:    GateRequirementState(decision.ObservedState),
			EvaluatedAt:      decision.EvaluatedAt,
			ObservedEvidence: decision.ObservedEvidence,
			Diagnostic:       optionalString(decision.Diagnostic),
		})
	}
	return result
}

func NewGateOverrideCollection(list gate.OverrideCollection, sources gate.SourceSet) GateOverrideCollection {
	eligible := make([]GateRequirement, 0, len(list.Eligible))
	for _, check := range list.Eligible {
		eligible = append(eligible, NewGateRequirement(check, sources))
	}
	return GateOverrideCollection{Eligible: eligible, Decisions: gateOverrideResponses(list.Decisions)}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
