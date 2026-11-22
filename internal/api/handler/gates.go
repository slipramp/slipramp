package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
)

func (h *Handler) gateSummaryResponse(summary gate.Summary) api.GateSummary {
	requirements := make([]api.GateRequirement, 0, len(summary))
	for _, requirement := range summary {
		requirements = append(requirements, h.gateRequirementResponse(requirement))
	}

	return api.GateSummary{
		State:        api.GateSummaryState(summary.State()),
		Requirements: requirements,
	}
}

func (h *Handler) gateRequirementResponse(requirement gate.Requirement) api.GateRequirement {
	var operator *api.GateOperator
	var threshold *float64
	if definition, ok := requirement.Policy.(*gate.PrometheusDefinition); ok {
		operator = new(api.GateOperator(definition.Condition.Operator))
		threshold = &definition.Condition.Value
	}
	return api.GateRequirement{
		Name:        api.CatalogName(requirement.Name),
		Key:         string(requirement.Key),
		Version:     optionalString(string(requirement.Version)),
		Type:        optionalString(requirement.Type),
		Origin:      api.GateRequirementOrigin(requirement.Origin),
		Scope:       api.GateScope{Kind: api.GateScopeKind(requirement.Scope.Kind), ID: requirement.Scope.ID},
		Overrides:   optionalSlice(gateOverrideResponses(requirement.Overrides)),
		QueryURL:    h.gateSources.QueryURL(requirement),
		Title:       optionalString(requirement.Title),
		State:       api.GateRequirementState(requirement.State),
		Evidence:    requirement.Evidence,
		Operator:    operator,
		Threshold:   threshold,
		EvaluatedAt: requirement.EvaluatedAt,
		ValidUntil:  requirement.ValidUntil,
		ReleasedAt:  requirement.ReleasedAt,
		Diagnostic:  optionalString(requirement.Diagnostic),
	}
}

func gateOverrideResponses(decisions []gate.Override) []api.GateOverride {
	result := make([]api.GateOverride, 0, len(decisions))
	for _, decision := range decisions {
		result = append(result, api.GateOverride{
			Key:              string(decision.Key),
			Version:          string(decision.Version),
			Actor:            *apiActorSnapshot(&decision.Actor),
			Reason:           decision.Reason,
			RecordedAt:       decision.RecordedAt,
			ObservedState:    api.GateRequirementState(decision.ObservedState),
			EvaluatedAt:      decision.EvaluatedAt,
			ObservedEvidence: decision.ObservedEvidence,
			Diagnostic:       optionalString(decision.Diagnostic),
		})
	}
	return result
}

type GateOverrideStore interface {
	ListGateOverrides(context.Context, gate.SubjectType, uuid.UUID, uuid.UUID) (gate.OverrideCollection, error)
	RecordGateOverride(context.Context, gate.OverrideRequest) (gate.Requirement, error)
}

func (h *Handler) overrideGate(ctx context.Context, subject gate.SubjectType, id uuid.UUID,
	body *api.GateOverrideRequest) (gate.Requirement, error) {
	if body == nil {
		return gate.Requirement{}, gate.ErrOverrideInvalid
	}
	principal := currentProductPrincipal(ctx)
	if principal == nil {
		return gate.Requirement{}, errors.New("authenticated Gate override actor is missing")
	}
	actor, err := principal.ActorSnapshot()
	if err != nil {
		return gate.Requirement{}, err
	}
	return h.gateOverrides.RecordGateOverride(ctx, gate.OverrideRequest{
		Subject:            subject,
		SubjectID:          id,
		Key:                gate.Key(body.Key),
		Version:            gate.Version(body.Version),
		Actor:              actor,
		RecordedAt:         h.now().UTC(),
		Reason:             body.Reason,
		EvaluatorProcessID: h.gateEvaluatorProcessID,
	})
}

func (h *Handler) OverrideRunGate(
	ctx context.Context,
	request api.OverrideRunGateRequestObject,
) (api.OverrideRunGateResponseObject, error) {
	requirement, err := h.overrideGate(ctx, gate.RunSubject, request.RunID, request.Body)
	switch {
	case err == nil:
		return api.OverrideRunGate200JSONResponse(h.gateRequirementResponse(requirement)), nil
	case errors.Is(err, gate.ErrOverrideConflict):
		return api.OverrideRunGate409ApplicationProblemPlusJSONResponse(NewProblem(http.StatusConflict, err.Error())), nil
	case errors.Is(err, gate.ErrSubjectNotFound):
		return api.OverrideRunGate404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrOverrideInvalid):
		return api.OverrideRunGate400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, err.Error())), nil
	default:
		return nil, err
	}
}

func (h *Handler) OverridePromotionGate(
	ctx context.Context,
	request api.OverridePromotionGateRequestObject,
) (api.OverridePromotionGateResponseObject, error) {
	requirement, err := h.overrideGate(ctx, gate.PromotionSubject, request.PromotionID, request.Body)
	switch {
	case err == nil:
		return api.OverridePromotionGate200JSONResponse(h.gateRequirementResponse(requirement)), nil
	case errors.Is(err, gate.ErrOverrideConflict):
		return api.OverridePromotionGate409ApplicationProblemPlusJSONResponse(NewProblem(http.StatusConflict, err.Error())), nil
	case errors.Is(err, gate.ErrSubjectNotFound):
		return api.OverridePromotionGate404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrOverrideInvalid):
		return api.OverridePromotionGate400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, err.Error())), nil
	default:
		return nil, err
	}
}

func (h *Handler) gateOverrideCollection(
	ctx context.Context, subject gate.SubjectType, id uuid.UUID,
) (api.GateOverrideCollection, error) {
	list, err := h.gateOverrides.ListGateOverrides(ctx, subject, id, h.gateEvaluatorProcessID)
	if err != nil {
		return api.GateOverrideCollection{}, err
	}
	eligible := make([]api.GateRequirement, 0, len(list.Eligible))
	for _, check := range list.Eligible {
		eligible = append(eligible, h.gateRequirementResponse(check))
	}
	return api.GateOverrideCollection{Eligible: eligible, Decisions: gateOverrideResponses(list.Decisions)}, nil
}

func (h *Handler) ListRunGateOverrides(
	ctx context.Context,
	request api.ListRunGateOverridesRequestObject,
) (api.ListRunGateOverridesResponseObject, error) {
	list, err := h.gateOverrideCollection(ctx, gate.RunSubject, request.RunID)
	if errors.Is(err, gate.ErrSubjectNotFound) {
		return api.ListRunGateOverrides404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	}
	if err != nil {
		return nil, err
	}
	return api.ListRunGateOverrides200JSONResponse(list), nil
}

func (h *Handler) ListPromotionGateOverrides(
	ctx context.Context,
	request api.ListPromotionGateOverridesRequestObject,
) (api.ListPromotionGateOverridesResponseObject, error) {
	list, err := h.gateOverrideCollection(ctx, gate.PromotionSubject, request.PromotionID)
	if errors.Is(err, gate.ErrSubjectNotFound) {
		return api.ListPromotionGateOverrides404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	}
	if err != nil {
		return nil, err
	}
	return api.ListPromotionGateOverrides200JSONResponse(list), nil
}
