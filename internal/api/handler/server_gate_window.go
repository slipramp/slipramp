package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
)

func (h *Handler) changeGateWindow(ctx context.Context, owner gate.Scope, window *gate.WeeklyWindow) (gate.ScopeState, error) {
	auth, err := currentProductAuthentication(ctx)
	if err != nil {
		return gate.ScopeState{}, err
	}
	return h.gateScopes.ChangeWindow(ctx, owner, auth, window)
}

func (h *Handler) SetGlobalGateWindow(ctx context.Context, request api.SetGlobalGateWindowRequestObject) (api.SetGlobalGateWindowResponseObject, error) {
	if request.Body == nil {
		return api.SetGlobalGateWindow400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, gate.ErrInvalidWindow.Error())), nil
	}
	state, err := h.changeGateWindow(ctx, gate.Scope{Kind: gate.GlobalScope}, request.Body)
	switch {
	case err == nil:
		return api.SetGlobalGateWindow200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.SetGlobalGateWindow404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.SetGlobalGateWindow403ApplicationProblemPlusJSONResponse(NewProblem(http.StatusForbidden, err.Error())), nil
	case errors.Is(err, gate.ErrInvalidWindow):
		return api.SetGlobalGateWindow400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, err.Error())), nil
	default:
		return nil, err
	}
}

func (h *Handler) RemoveGlobalGateWindow(ctx context.Context, request api.RemoveGlobalGateWindowRequestObject) (api.RemoveGlobalGateWindowResponseObject, error) {
	state, err := h.changeGateWindow(ctx, gate.Scope{Kind: gate.GlobalScope}, nil)
	switch {
	case err == nil:
		return api.RemoveGlobalGateWindow200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.RemoveGlobalGateWindow404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.RemoveGlobalGateWindow403ApplicationProblemPlusJSONResponse(NewProblem(http.StatusForbidden, err.Error())), nil
	default:
		return nil, err
	}
}

func (h *Handler) SetTargetGateWindow(ctx context.Context, request api.SetTargetGateWindowRequestObject) (api.SetTargetGateWindowResponseObject, error) {
	if request.Body == nil {
		return api.SetTargetGateWindow400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, gate.ErrInvalidWindow.Error())), nil
	}
	state, err := h.changeGateWindow(ctx, gate.Scope{Kind: gate.TargetScope, ID: request.TargetID}, request.Body)
	switch {
	case err == nil:
		return api.SetTargetGateWindow200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.SetTargetGateWindow404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.SetTargetGateWindow403ApplicationProblemPlusJSONResponse(NewProblem(http.StatusForbidden, err.Error())), nil
	case errors.Is(err, gate.ErrInvalidWindow):
		return api.SetTargetGateWindow400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, err.Error())), nil
	default:
		return nil, err
	}
}

func (h *Handler) RemoveTargetGateWindow(ctx context.Context, request api.RemoveTargetGateWindowRequestObject) (api.RemoveTargetGateWindowResponseObject, error) {
	state, err := h.changeGateWindow(ctx, gate.Scope{Kind: gate.TargetScope, ID: request.TargetID}, nil)
	switch {
	case err == nil:
		return api.RemoveTargetGateWindow200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.RemoveTargetGateWindow404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.RemoveTargetGateWindow403ApplicationProblemPlusJSONResponse(NewProblem(http.StatusForbidden, err.Error())), nil
	default:
		return nil, err
	}
}

func (h *Handler) SetStageGateWindow(ctx context.Context, request api.SetStageGateWindowRequestObject) (api.SetStageGateWindowResponseObject, error) {
	if request.Body == nil {
		return api.SetStageGateWindow400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, gate.ErrInvalidWindow.Error())), nil
	}
	state, err := h.changeGateWindow(ctx, gate.Scope{Kind: gate.StageScope, ID: request.StageID}, request.Body)
	switch {
	case err == nil:
		return api.SetStageGateWindow200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.SetStageGateWindow404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.SetStageGateWindow403ApplicationProblemPlusJSONResponse(NewProblem(http.StatusForbidden, err.Error())), nil
	case errors.Is(err, gate.ErrInvalidWindow):
		return api.SetStageGateWindow400ApplicationProblemPlusJSONResponse(NewProblem(http.StatusBadRequest, err.Error())), nil
	default:
		return nil, err
	}
}

func (h *Handler) RemoveStageGateWindow(ctx context.Context, request api.RemoveStageGateWindowRequestObject) (api.RemoveStageGateWindowResponseObject, error) {
	state, err := h.changeGateWindow(ctx, gate.Scope{Kind: gate.StageScope, ID: request.StageID}, nil)
	switch {
	case err == nil:
		return api.RemoveStageGateWindow200JSONResponse(api.NewServerGateScope(state)), nil
	case errors.Is(err, gate.ErrScopeNotFound):
		return api.RemoveStageGateWindow404ApplicationProblemPlusJSONResponse(NewProblem(http.StatusNotFound, err.Error())), nil
	case errors.Is(err, gate.ErrScopeForbidden):
		return api.RemoveStageGateWindow403ApplicationProblemPlusJSONResponse(NewProblem(http.StatusForbidden, err.Error())), nil
	default:
		return nil, err
	}
}
