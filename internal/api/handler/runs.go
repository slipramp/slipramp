package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

type RunService interface {
	CreateRun(ctx context.Context, request run.CreateRequest) (run.Run, error)
	GetRun(context.Context, uuid.UUID) (run.Run, error)
	ListRuns(context.Context, run.ListFilter) ([]run.Run, error)
	CancelRun(ctx context.Context, request run.CancelRequest) (run.Run, error)
	NextRunAssignment(ctx context.Context, clusterName string) (run.Run, error)
	NextRunToCancel(ctx context.Context, clusterName string) (run.Run, error)
	ReportObservation(ctx context.Context, observation run.Observation) error
}

func (h *Handler) ListRuns(
	ctx context.Context,
	request api.ListRunsRequestObject,
) (api.ListRunsResponseObject, error) {
	limit := int32(run.DefaultListLimit)
	if request.Params.Limit != nil {
		if *request.Params.Limit < 1 || *request.Params.Limit > run.MaximumListLimit {
			problem := NewProblem(
				http.StatusBadRequest,
				fmt.Sprintf("limit must be between 1 and %d", run.MaximumListLimit),
			)

			return api.ListRuns400ApplicationProblemPlusJSONResponse(problem), nil
		}
		limit = int32(*request.Params.Limit)
	}
	targetName := ""
	if request.Params.Target != nil {
		targetName = string(*request.Params.Target)
	}

	records, err := h.runs.ListRuns(
		ctx,
		run.ListFilter{
			RepositoryID: request.Params.RepositoryID,
			TargetName:   targetName,
			Limit:        limit,
		},
	)
	if errors.Is(err, run.ErrInvalidRequest) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListRuns400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	items, err := h.runResponses(ctx, records)
	if err != nil {
		return nil, err
	}

	return api.ListRuns200JSONResponse{Items: items}, nil
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
			RepositoryID:   request.Body.RepositoryID,
			TargetName:     string(request.Body.Target),
			ActionName:     string(request.Body.Action),
			Parameters:     parameters,
			Inputs:         runInputs(request.Body.Inputs),
			Source:         currentRunSource(ctx),
			IdempotencyKey: request.Params.IdempotencyKey,
			Actor:          currentProductPrincipal(ctx),
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, run.ErrInvalidRequest),
			errors.Is(err, run.ErrInvalidParameters),
			errors.Is(err, run.ErrInvalidInputs):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.CreateRun400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, run.ErrRepositoryNotFound),
			errors.Is(err, run.ErrCatalogNotFound),
			errors.Is(err, run.ErrTargetNotFound),
			errors.Is(err, run.ErrActionNotFound),
			errors.Is(err, run.ErrRuntimeNotFound),
			errors.Is(err, run.ErrPlacementNotFound),
			errors.Is(err, run.ErrArtifactNotFound):
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

	body, err := h.runResponse(ctx, created)
	if err != nil {
		return nil, err
	}

	return api.CreateRun201JSONResponse(body), nil
}

func currentRunSource(ctx context.Context) run.Source {
	authentication, authenticated := productAuthenticationFromContext(ctx)
	if authenticated && authentication.Method == productidentity.AuthenticationMethodAPIKey {
		return run.Source{Type: run.SourceTypeAPI}
	}

	return run.Source{Type: run.SourceTypeManual}
}

func (h *Handler) GetRun(
	ctx context.Context,
	request api.GetRunRequestObject,
) (api.GetRunResponseObject, error) {
	record, err := h.runs.GetRun(ctx, request.RunID)
	if errors.Is(err, run.ErrRunNotFound) {
		problem := NewProblem(http.StatusNotFound, "The Run does not exist.")

		return api.GetRun404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	body, err := h.runResponse(ctx, record)
	if err != nil {
		return nil, err
	}

	return api.GetRun200JSONResponse(body), nil
}

func runInputs(values *map[string]uuid.UUID) map[string]uuid.UUID {
	if values == nil {
		return map[string]uuid.UUID{}
	}

	result := make(map[string]uuid.UUID, len(*values))
	maps.Copy(result, *values)

	return result
}

func (h *Handler) CancelRun(
	ctx context.Context,
	request api.CancelRunRequestObject,
) (api.CancelRunResponseObject, error) {
	record, err := h.runs.CancelRun(
		ctx,
		run.CancelRequest{
			RunID: request.RunID,
			Actor: currentProductPrincipal(ctx),
		},
	)
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

	body, err := h.runResponse(ctx, record)
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
		CreatedBy:               apiRunActor(record.CreatedBy),
		CanceledBy:              apiRunActor(record.CanceledBy),
		Inputs:                  runInputResponse(record.Snapshot.Inputs),
		ArtifactConsumers:       runArtifactConsumers(record.Snapshot),
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

func apiRunActor(value *productidentity.ActorSnapshot) *api.RunActor {
	if value == nil {
		return nil
	}

	return &api.RunActor{
		PrincipalID: value.PrincipalID,
		Kind:        api.RunActorKind(value.Kind),
		DisplayName: value.DisplayName,
	}
}

func (h *Handler) runResponse(ctx context.Context, record run.Run) (api.Run, error) {
	items, err := h.runResponses(ctx, []run.Run{record})
	if err != nil {
		return api.Run{}, err
	}

	return items[0], nil
}

func (h *Handler) runResponses(ctx context.Context, records []run.Run) ([]api.Run, error) {
	needsClusterStatus := false
	for _, record := range records {
		if record.Phase != run.PhaseQueued && record.Phase != run.PhaseFinished {
			needsClusterStatus = true
			break
		}
	}

	connectedClusters := make(map[string]bool)
	if needsClusterStatus {
		clusters, err := h.clusters.ListClusters(ctx)
		if err != nil {
			return nil, fmt.Errorf("load cluster status for Runs: %w", err)
		}
		for _, status := range clusters {
			connectedClusters[status.Name] =
				status.ConnectionState == cluster.ConnectionStateConnected
		}
	}

	items := make([]api.Run, 0, len(records))
	now := h.now().UTC()
	for _, record := range records {
		body, err := runResponse(record)
		if err != nil {
			return nil, err
		}

		state, present := run.DeriveObservationState(
			record,
			connectedClusters[record.ClusterName],
			now,
		)
		if present {
			value := api.RunObservationState(state)
			body.ObservationState = &value
		}

		items = append(items, body)
	}

	return items, nil
}

func runArtifactConsumers(snapshot run.Snapshot) map[string][]api.RunArtifactConsumer {
	result := make(map[string][]api.RunArtifactConsumer)
	for _, actionName := range slices.Sorted(maps.Keys(snapshot.Target.Spec.Actions)) {
		action := snapshot.Target.Spec.Actions[actionName]
		if len(action.Inputs) != 1 || hasRequiredParameters(action) {
			continue
		}

		for inputName, input := range action.Inputs {
			if input.Artifact.FromAction != snapshot.Action.Name {
				continue
			}
			if _, declared := snapshot.Action.Artifacts[inputName]; !declared {
				continue
			}

			result[inputName] = append(result[inputName], api.RunArtifactConsumer{
				Action: api.CatalogName(actionName),
				Input:  api.CatalogName(inputName),
			})
		}
	}

	return result
}

func hasRequiredParameters(action catalog.Action) bool {
	for _, parameter := range action.Parameters {
		if parameter.Required {
			return true
		}
	}

	return false
}

func runInputResponse(values map[string]run.ArtifactInput) map[string]api.RunArtifactInput {
	result := make(map[string]api.RunArtifactInput, len(values))
	for name, value := range values {
		result[name] = api.RunArtifactInput{
			ArtifactID:        value.ArtifactID,
			ProducerRunID:     value.ProducerRunID,
			Name:              api.CatalogName(value.Name),
			MediaType:         value.MediaType,
			SizeBytes:         value.SizeBytes,
			Sha256:            value.SHA256,
			Sensitive:         value.Sensitive,
			RetentionState:    api.RunArtifactInputRetentionState(value.RetentionState),
			CommitSha:         value.CommitSHA,
			CatalogRevisionID: value.CatalogRevisionID,
		}
	}

	return result
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
