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
		requirements = append(requirements, api.NewGateRequirement(requirement, h.gateSources))
	}

	return api.GateSummary{
		State:        api.GateSummaryState(summary.State()),
		Requirements: requirements,
	}
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
		Reason:             valueOrEmptyString(body.Reason),
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
		return api.OverrideRunGate200JSONResponse(api.NewGateRequirement(requirement, h.gateSources)), nil
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
		return api.OverridePromotionGate200JSONResponse(api.NewGateRequirement(requirement, h.gateSources)), nil
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
	return api.NewGateOverrideCollection(list, h.gateSources), nil
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
