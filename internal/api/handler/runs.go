package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/run"
)

type RunService interface {
	CreateRun(ctx context.Context, request run.CreateRequest) (run.Run, error)
	CancelRun(ctx context.Context, runID uuid.UUID) (run.Run, error)
	NextRunAssignment(ctx context.Context, clusterName string) (run.Run, error)
	NextRunToCancel(ctx context.Context, clusterName string) (run.Run, error)
	ReportObservation(ctx context.Context, observation run.Observation) error
}

func (h *Handler) CreateRun(
	ctx context.Context,
	request api.CreateRunRequestObject,
) (api.CreateRunResponseObject, error) {
	parameters, err := runParameters(request.Body.Parameters)
	if err != nil {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.CreateRun400ApplicationProblemPlusJSONResponse(problem), nil
	}

	created, err := h.runs.CreateRun(
		ctx,
		run.CreateRequest{
			RepositoryID: request.Body.RepositoryID,
			TargetName:   string(request.Body.Target),
			ActionName:   string(request.Body.Action),
			Parameters:   parameters,
			Source: run.Source{
				Type: run.SourceType(request.Body.Source.Type),
			},
			IdempotencyKey: request.Params.IdempotencyKey,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, run.ErrInvalidRequest),
			errors.Is(err, run.ErrInvalidParameters):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.CreateRun400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, run.ErrRepositoryNotFound),
			errors.Is(err, run.ErrCatalogNotFound),
			errors.Is(err, run.ErrTargetNotFound),
			errors.Is(err, run.ErrActionNotFound),
			errors.Is(err, run.ErrRuntimeNotFound),
			errors.Is(err, run.ErrPlacementNotFound):
			problem := NewProblem(
				http.StatusNotFound,
				"The requested action could not be resolved from the active catalog.",
			)

			return api.CreateRun404ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, run.ErrIdempotencyConflict):
			problem := NewProblem(
				http.StatusConflict,
				"The idempotency key was already used for a different run request.",
			)

			return api.CreateRun409ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}

	body, err := runResponse(created)
	if err != nil {
		return nil, err
	}

	return api.CreateRun201JSONResponse(body), nil
}

func (h *Handler) CancelRun(
	ctx context.Context,
	request api.CancelRunRequestObject,
) (api.CancelRunResponseObject, error) {
	record, err := h.runs.CancelRun(ctx, request.RunID)
	if err != nil {
		switch {
		case errors.Is(err, run.ErrRunNotFound):
			problem := NewProblem(http.StatusNotFound, "The Run does not exist.")

			return api.CancelRun404ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, run.ErrRunFinished):
			problem := NewProblem(
				http.StatusConflict,
				"The Run already finished and cannot be canceled.",
			)

			return api.CancelRun409ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}

	body, err := runResponse(record)
	if err != nil {
		return nil, err
	}

	return api.CancelRun200JSONResponse(body), nil
}

func runParameters(
	values *map[string]api.ParameterValue,
) (map[string]any, error) {
	if values == nil {
		return map[string]any{}, nil
	}

	parameters := make(map[string]any, len(*values))
	for name, value := range *values {
		encoded, err := value.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("encode parameter %q: %w", name, err)
		}

		var decoded any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return nil, fmt.Errorf("decode parameter %q: %w", name, err)
		}

		parameters[name] = decoded
	}

	return parameters, nil
}

func runResponse(record run.Run) (api.Run, error) {
	parameters, err := runParameterResponse(record.Parameters)
	if err != nil {
		return api.Run{}, err
	}

	return api.Run{
		ID:                record.ID,
		Phase:             api.RunPhase(record.Phase),
		Outcome:           apiRunOutcome(record.Outcome),
		RepositoryID:      record.RepositoryID,
		CatalogRevisionID: record.CatalogRevisionID,
		CommitSha:         record.CommitSHA,
		Target:            api.CatalogName(record.TargetName),
		Action:            api.CatalogName(record.ActionName),
		Parameters:        parameters,
		Placement:         api.CatalogName(record.PlacementName),
		Cluster:           api.CatalogName(record.ClusterName),
		Source: api.RunSource{
			Type: api.RunSourceType(record.Source.Type),
		},
		CreatedAt:               record.CreatedAt,
		AssignedAt:              record.AssignedAt,
		PreparingAt:             record.PreparingAt,
		StartedAt:               record.StartedAt,
		FinishedAt:              record.FinishedAt,
		LastObservedAt:          record.LastObservedAt,
		CancellationRequestedAt: record.CancellationRequestedAt,
		JobUID:                  record.JobUID,
		OutcomeReason:           record.OutcomeReason,
		OutcomeMessage:          record.OutcomeMessage,
		ExitCode:                record.ExitCode,
	}, nil
}

func apiRunOutcome(value *run.Outcome) *api.RunOutcome {
	if value == nil {
		return nil
	}

	converted := api.RunOutcome(*value)

	return &converted
}

func runParameterResponse(
	values map[string]any,
) (map[string]api.ParameterValue, error) {
	parameters := make(map[string]api.ParameterValue, len(values))
	for name, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode Run parameter %q: %w", name, err)
		}

		var parameter api.ParameterValue
		if err := parameter.UnmarshalJSON(encoded); err != nil {
			return nil, fmt.Errorf("convert Run parameter %q: %w", name, err)
		}

		parameters[name] = parameter
	}

	return parameters, nil
}
