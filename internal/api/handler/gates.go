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
		var operator *api.GateOperator
		var threshold *float64
		if definition := requirement.Prometheus; definition != nil {
			operator = new(api.GateOperator(definition.Condition.Operator))
			threshold = &definition.Condition.Value
		}
		requirements = append(requirements, api.GateRequirement{
			Name:             api.CatalogName(requirement.Name),
			DefinitionDigest: optionalString(requirement.DefinitionDigest),
			Overrides:        gateOverrideResponses(requirement.Overrides),
			QueryURL:         h.gateSources.QueryURL(requirement),
			Title:            optionalString(requirement.Title),
			State:            api.GateRequirementState(requirement.State),
			Value:            requirement.Value,
			Operator:         operator,
			Threshold:        threshold,
			EvaluatedAt:      requirement.EvaluatedAt,
			ReleasedAt:       requirement.ReleasedAt,
			Diagnostic:       optionalString(requirement.Diagnostic),
		})
	}

	return api.GateSummary{
		State:        api.GateSummaryState(summary.State()),
		Requirements: requirements,
	}
}

func gateOverrideResponses(decisions []gate.Override) *[]api.GateOverride {
	if len(decisions) == 0 {
		return nil
	}
	result := make([]api.GateOverride, 0, len(decisions))
	for _, decision := range decisions {
		result = append(result, api.GateOverride{
			Actor: *apiActorSnapshot(&decision.Actor), Reason: decision.Reason, RecordedAt: decision.RecordedAt,
			DefinitionDigest: decision.DefinitionDigest, ObservedState: api.GateRequirementState(decision.ObservedState),
			EvaluatedAt: decision.EvaluatedAt, ObservedValue: decision.ObservedValue, Diagnostic: optionalString(decision.Diagnostic),
		})
	}
	return &result
}

type GateOverrideStore interface {
	RecordGateOverride(context.Context, gate.OverrideRequest) error
}

func (h *Handler) overrideGate(ctx context.Context, subject gate.SubjectType, id uuid.UUID, name api.CatalogName,
	body *api.GateOverrideRequest) error {
	if body == nil {
		return gate.ErrOverrideInvalid
	}
	principal := currentProductPrincipal(ctx)
	if principal == nil {
		return errors.New("authenticated Gate override actor is missing")
	}
	actor, err := principal.ActorSnapshot()
	if err != nil {
		return err
	}
	return h.gateOverrides.RecordGateOverride(ctx, gate.OverrideRequest{
		Subject: subject, SubjectID: id, GateName: string(name), Actor: actor, RecordedAt: h.now().UTC(),
		DefinitionDigest: body.DefinitionDigest, Reason: body.Reason, EvaluatorProcessID: h.gateEvaluatorProcessID,
	})
}

func (h *Handler) OverrideRunGate(
	ctx context.Context,
	request api.OverrideRunGateRequestObject,
) (api.OverrideRunGateResponseObject, error) {
	err := h.overrideGate(ctx, gate.RunSubject, request.RunID, request.GateName, request.Body)
	switch {
	case err == nil:
		return api.OverrideRunGate204Response{}, nil
	case errors.Is(err, gate.ErrOverrideConflict):
		return api.OverrideRunGate409ApplicationProblemPlusJSONResponse(NewProblem(http.StatusConflict, err.Error())), nil
	case errors.Is(err, gate.ErrRequirementNotFound):
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
	err := h.overrideGate(ctx, gate.PromotionSubject, request.PromotionID, request.GateName, request.Body)
	switch {
	case err == nil:
		return api.OverridePromotionGate204Response{}, nil
	case errors.Is(err, gate.ErrOverrideConflict):
		return api.OverridePromotionGate409ApplicationProblemPlusJSONResponse(NewProblem(http.StatusConflict, err.Error())), nil
	case errors.Is(err, gate.ErrRequirementNotFound):
		return api.OverridePromotionGate404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrOverrideInvalid):
		return api.OverridePromotionGate400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, err.Error())), nil
	default:
		return nil, err
	}
}
