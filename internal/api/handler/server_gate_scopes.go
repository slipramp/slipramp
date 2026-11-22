package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
)

func (h *Handler) changeServerGateLock(
	ctx context.Context,
	owner gate.Scope,
	request *gate.LockRequest,
) (gate.ScopeState, error) {
	auth, err := currentProductAuthentication(ctx)
	if err != nil {
		return gate.ScopeState{}, err
	}
	return h.gateScopes.ChangeLock(ctx, owner, auth, request)
}

func (h *Handler) putServerGateLock(
	ctx context.Context,
	owner gate.Scope,
	body *api.ServerGateLockRequest,
) (gate.ScopeState, error) {
	if body == nil {
		return gate.ScopeState{}, gate.ErrLockReason
	}
	return h.changeServerGateLock(ctx, owner, body)
}

func (h *Handler) GetGlobalGateScope(
	ctx context.Context,
	_ api.GetGlobalGateScopeRequestObject,
) (api.GetGlobalGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.GlobalScope}
	state, err := h.gateScopes.Inspect(ctx, owner)
	switch {
	case err == nil:
		return api.GetGlobalGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.GetGlobalGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	default:
		return nil, err
	}
}

func (h *Handler) LockGlobalGateScope(
	ctx context.Context,
	request api.LockGlobalGateScopeRequestObject,
) (api.LockGlobalGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.GlobalScope}
	state, err := h.putServerGateLock(ctx, owner, request.Body)
	switch {
	case err == nil:
		return api.LockGlobalGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.LockGlobalGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.LockGlobalGateScope403ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusForbidden, err.Error()),
		), nil
	case errors.Is(err, gate.ErrLockReason), errors.Is(err, gate.ErrLockExpiry):
		return api.LockGlobalGateScope400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	case errors.Is(err, gate.ErrLockConflict):
		return api.LockGlobalGateScope409ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusConflict, err.Error()),
		), nil
	default:
		return nil, err
	}
}

func (h *Handler) UnlockGlobalGateScope(
	ctx context.Context,
	_ api.UnlockGlobalGateScopeRequestObject,
) (api.UnlockGlobalGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.GlobalScope}
	state, err := h.changeServerGateLock(ctx, owner, nil)
	switch {
	case err == nil:
		return api.UnlockGlobalGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.UnlockGlobalGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.UnlockGlobalGateScope403ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusForbidden, err.Error()),
		), nil
	default:
		return nil, err
	}
}

func (h *Handler) GetTargetGateScope(
	ctx context.Context,
	request api.GetTargetGateScopeRequestObject,
) (api.GetTargetGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.TargetScope, ID: request.TargetID}
	state, err := h.gateScopes.Inspect(ctx, owner)
	switch {
	case err == nil:
		return api.GetTargetGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.GetTargetGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	default:
		return nil, err
	}
}

func (h *Handler) LockTargetGateScope(
	ctx context.Context,
	request api.LockTargetGateScopeRequestObject,
) (api.LockTargetGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.TargetScope, ID: request.TargetID}
	state, err := h.putServerGateLock(ctx, owner, request.Body)
	switch {
	case err == nil:
		return api.LockTargetGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.LockTargetGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.LockTargetGateScope403ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusForbidden, err.Error()),
		), nil
	case errors.Is(err, gate.ErrLockReason), errors.Is(err, gate.ErrLockExpiry):
		return api.LockTargetGateScope400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	case errors.Is(err, gate.ErrLockConflict):
		return api.LockTargetGateScope409ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusConflict, err.Error()),
		), nil
	default:
		return nil, err
	}
}

func (h *Handler) UnlockTargetGateScope(
	ctx context.Context,
	request api.UnlockTargetGateScopeRequestObject,
) (api.UnlockTargetGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.TargetScope, ID: request.TargetID}
	state, err := h.changeServerGateLock(ctx, owner, nil)
	switch {
	case err == nil:
		return api.UnlockTargetGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.UnlockTargetGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.UnlockTargetGateScope403ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusForbidden, err.Error()),
		), nil
	default:
		return nil, err
	}
}

func (h *Handler) GetStageGateScope(
	ctx context.Context,
	request api.GetStageGateScopeRequestObject,
) (api.GetStageGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.StageScope, ID: request.StageID}
	state, err := h.gateScopes.Inspect(ctx, owner)
	switch {
	case err == nil:
		return api.GetStageGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.GetStageGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	default:
		return nil, err
	}
}

func (h *Handler) LockStageGateScope(
	ctx context.Context,
	request api.LockStageGateScopeRequestObject,
) (api.LockStageGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.StageScope, ID: request.StageID}
	state, err := h.putServerGateLock(ctx, owner, request.Body)
	switch {
	case err == nil:
		return api.LockStageGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.LockStageGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.LockStageGateScope403ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusForbidden, err.Error()),
		), nil
	case errors.Is(err, gate.ErrLockReason), errors.Is(err, gate.ErrLockExpiry):
		return api.LockStageGateScope400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	case errors.Is(err, gate.ErrLockConflict):
		return api.LockStageGateScope409ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusConflict, err.Error()),
		), nil
	default:
		return nil, err
	}
}

func (h *Handler) UnlockStageGateScope(
	ctx context.Context,
	request api.UnlockStageGateScopeRequestObject,
) (api.UnlockStageGateScopeResponseObject, error) {
	owner := gate.Scope{Kind: gate.StageScope, ID: request.StageID}
	state, err := h.changeServerGateLock(ctx, owner, nil)
	switch {
	case err == nil:
		return api.UnlockStageGateScope200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.UnlockStageGateScope404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, err.Error()),
		), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.UnlockStageGateScope403ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusForbidden, err.Error()),
		), nil
	default:
		return nil, err
	}
}
