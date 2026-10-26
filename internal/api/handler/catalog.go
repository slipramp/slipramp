package handler

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/repository"
)

type CatalogService interface {
	ExplainAction(
		context.Context,
		uuid.UUID,
		string,
	) (catalog.Explanation, error)
	GetActive(ctx context.Context, repositoryID uuid.UUID) (catalog.ActiveCatalog, error)
	GetTarget(context.Context, uuid.UUID) (catalog.TargetSummary, error)
	ListTargets(context.Context, catalog.TargetListFilter) (catalog.TargetList, error)
}

func (h *Handler) ExplainTargetAction(
	ctx context.Context,
	request api.ExplainTargetActionRequestObject,
) (api.ExplainTargetActionResponseObject, error) {
	actionName := string(request.ActionName)
	if err := kubevalidate.DNSLabel("action name", actionName); err != nil {
		problem := NewProblem(http.StatusBadRequest, "actionName must be a valid catalog name")

		return api.ExplainTargetAction400ApplicationProblemPlusJSONResponse(problem), nil
	}

	explanation, err := h.catalogs.ExplainAction(
		ctx,
		request.TargetID,
		actionName,
	)
	if errors.Is(err, catalog.ErrNotFound) ||
		errors.Is(err, catalog.ErrTargetNotFound) ||
		errors.Is(err, catalog.ErrActionNotFound) {
		problem := NewProblem(
			http.StatusNotFound,
			"The active repository catalog, Target, or Action does not exist.",
		)

		return api.ExplainTargetAction404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.ExplainTargetAction200JSONResponse(explanation), nil
}

func (h *Handler) ListTargets(
	ctx context.Context,
	request api.ListTargetsRequestObject,
) (api.ListTargetsResponseObject, error) {
	limit := catalog.DefaultTargetListLimit
	if request.Params.Limit != nil {
		if *request.Params.Limit < 1 ||
			*request.Params.Limit > catalog.MaximumTargetListLimit {
			problem := NewProblem(
				http.StatusBadRequest,
				fmt.Sprintf("limit must be between 1 and %d", catalog.MaximumTargetListLimit),
			)

			return api.ListTargets400ApplicationProblemPlusJSONResponse(problem), nil
		}
		limit = *request.Params.Limit
	}

	offset := int32(0)
	if request.Params.Offset != nil {
		if *request.Params.Offset < 0 ||
			*request.Params.Offset > catalog.MaximumTargetListOffset {
			problem := NewProblem(http.StatusBadRequest, "offset must be between 0 and 2147483647")

			return api.ListTargets400ApplicationProblemPlusJSONResponse(problem), nil
		}
		offset = *request.Params.Offset
	}

	query := ""
	if request.Params.Q != nil {
		query = strings.TrimSpace(*request.Params.Q)
		if query == "" || len(query) > catalog.MaximumTargetSearchLength {
			problem := NewProblem(http.StatusBadRequest, "q must contain between 1 and 200 characters")

			return api.ListTargets400ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	runtime, problem := targetCatalogNameFilter(request.Params.Runtime, "runtime")
	if problem != nil {
		return api.ListTargets400ApplicationProblemPlusJSONResponse(*problem), nil
	}
	placement, problem := targetCatalogNameFilter(request.Params.Placement, "placement")
	if problem != nil {
		return api.ListTargets400ApplicationProblemPlusJSONResponse(*problem), nil
	}

	targets, err := h.catalogs.ListTargets(
		ctx,
		catalog.TargetListFilter{
			RepositoryID: request.Params.RepositoryID,
			Query:        query,
			Runtime:      runtime,
			Placement:    placement,
			Limit:        int32(limit),
			Offset:       int32(offset),
		},
	)
	if err != nil {
		return nil, err
	}
	keys := make([]repository.CommitKey, 0, len(targets.Items)*2)
	for _, item := range targets.Items {
		keys = append(
			keys,
			commitKey(item.RepositoryID, item.CatalogCommitSHA),
			commitKey(item.RepositoryID, item.DesiredCommitSHA),
		)
		keys = appendTargetCheckpointCommitKeys(keys, item.RepositoryID, item.LastAttemptedSync)
		keys = appendTargetCheckpointCommitKeys(keys, item.RepositoryID, item.LastSuccessfulSync)
	}
	commits, err := h.commitResponses(ctx, keys)
	if err != nil {
		return nil, err
	}

	items := make([]api.TargetSummary, 0, len(targets.Items))
	for _, item := range targets.Items {
		items = append(items, api.TargetSummary{
			Actions:           slices.Sorted(maps.Keys(item.Target.Spec.Actions)),
			CatalogCommit:     commits[commitKey(item.RepositoryID, item.CatalogCommitSHA)],
			CatalogRevisionID: item.CatalogRevisionID,
			DesiredCommit:     commits[commitKey(item.RepositoryID, item.DesiredCommitSHA)],
			Reconciliation:    targetReconciliationResponse(item, commits),
			ID:                item.ID,
			Name:              item.Target.Metadata.Name,
			Placement:         optionalString(item.Target.Spec.Placement),
			RepositoryID:      item.RepositoryID,
			RepositoryName:    item.RepositoryName,
			Runtime:           optionalString(item.Target.Spec.Runtime),
			Title:             optionalString(item.Target.Metadata.Title),
		})
	}

	return api.ListTargets200JSONResponse{
		Items:  items,
		Limit:  limit,
		Offset: int(offset),
		Total:  targets.Total,
	}, nil
}

func (h *Handler) GetTarget(
	ctx context.Context,
	request api.GetTargetRequestObject,
) (api.GetTargetResponseObject, error) {
	record, err := h.catalogs.GetTarget(ctx, request.TargetID)
	if errors.Is(err, catalog.ErrTargetNotFound) {
		problem := NewProblem(http.StatusNotFound, "The Target does not exist.")

		return api.GetTarget404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	keys := []repository.CommitKey{
		commitKey(record.RepositoryID, record.CatalogCommitSHA),
		commitKey(record.RepositoryID, record.DesiredCommitSHA),
	}
	keys = appendTargetCheckpointCommitKeys(keys, record.RepositoryID, record.LastAttemptedSync)
	keys = appendTargetCheckpointCommitKeys(keys, record.RepositoryID, record.LastSuccessfulSync)
	commits, err := h.commitResponses(ctx, keys)
	if err != nil {
		return nil, err
	}
	definition, err := catalogTargetResponse(
		record.Target,
		catalog.TargetState{DesiredCommitSHA: record.DesiredCommitSHA},
		commits,
		record.RepositoryID,
		record.ArchivedAt != nil,
		record.LastAttemptedSync,
		record.LastSuccessfulSync,
	)
	if err != nil {
		return nil, err
	}

	return api.GetTarget200JSONResponse{
		ArchivedAt:        record.ArchivedAt,
		CatalogCommit:     commits[commitKey(record.RepositoryID, record.CatalogCommitSHA)],
		CatalogRevisionID: record.CatalogRevisionID,
		Definition:        definition,
		ID:                record.ID,
		RepositoryID:      record.RepositoryID,
		RepositoryName:    record.RepositoryName,
	}, nil
}

func targetCatalogNameFilter(value *string, name string) (string, *api.Problem) {
	if value == nil {
		return "", nil
	}

	trimmed := strings.TrimSpace(*value)
	if err := kubevalidate.DNSLabel(name, trimmed); err != nil {
		problem := NewProblem(http.StatusBadRequest, name+" must be a valid catalog name")

		return "", &problem
	}

	return trimmed, nil
}

func (h *Handler) GetRepositoryCatalog(
	ctx context.Context,
	request api.GetRepositoryCatalogRequestObject,
) (api.GetRepositoryCatalogResponseObject, error) {
	active, err := h.catalogs.GetActive(ctx, request.RepositoryID)
	if errors.Is(err, catalog.ErrNotFound) {
		problem := NewProblem(
			http.StatusNotFound,
			"The active repository does not exist or does not have an accepted catalog revision.",
		)

		return api.GetRepositoryCatalog404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	keys := []repository.CommitKey{commitKey(request.RepositoryID, active.Revision.CommitSHA)}
	for _, state := range active.TargetStates {
		keys = append(keys, commitKey(request.RepositoryID, state.DesiredCommitSHA))
		keys = appendTargetCheckpointCommitKeys(keys, request.RepositoryID, state.LastAttemptedSync)
		keys = appendTargetCheckpointCommitKeys(keys, request.RepositoryID, state.LastSuccessfulSync)
	}
	commits, err := h.commitResponses(ctx, keys)
	if err != nil {
		return nil, err
	}

	body, err := activeCatalogResponse(request.RepositoryID, active, commits)
	if err != nil {
		return nil, err
	}

	return api.GetRepositoryCatalog200JSONResponse(body), nil
}

func activeCatalogResponse(
	repositoryID uuid.UUID,
	active catalog.ActiveCatalog,
	commits map[repository.CommitKey]api.GitCommit,
) (api.CatalogRevision, error) {
	revision := active.Revision
	runtimes := make([]api.Runtime, 0, len(revision.Catalog.Runtimes))
	for _, runtime := range revision.Catalog.Runtimes {
		runtimes = append(runtimes, catalogRuntimeResponse(runtime))
	}

	targets := make([]api.TargetDefinition, 0, len(revision.Catalog.Targets))
	for _, target := range revision.Catalog.Targets {
		converted, err := catalogTargetResponse(
			target,
			active.TargetStates[target.Metadata.Name],
			commits,
			repositoryID,
			false,
			active.TargetStates[target.Metadata.Name].LastAttemptedSync,
			active.TargetStates[target.Metadata.Name].LastSuccessfulSync,
		)
		if err != nil {
			return api.CatalogRevision{}, err
		}
		targets = append(targets, converted)
	}

	return api.CatalogRevision{
		ID:        revision.ID,
		Commit:    commits[commitKey(repositoryID, revision.CommitSHA)],
		CreatedAt: revision.CreatedAt,
		Runtimes:  runtimes,
		Targets:   targets,
	}, nil
}

func catalogRuntimeResponse(runtime catalog.Runtime) api.Runtime {
	credentials := make(map[string]api.RuntimeCredential)
	for name, credential := range runtime.Spec.Credentials {
		credentials[name] = api.RuntimeCredential{
			Inject: catalogCredentialInjectionResponse(credential.Inject),
		}
	}

	return api.Runtime{
		APIVersion: api.RuntimeAPIVersion(runtime.APIVersion),
		Kind:       api.RuntimeKind(runtime.Kind),
		Metadata:   catalogMetadataResponse(runtime.Metadata),
		Spec: api.RuntimeSpec{
			Image:            runtime.Spec.Image,
			WorkingDirectory: optionalString(runtime.Spec.WorkingDirectory),
			Environment:      optionalStringMap(runtime.Spec.Environment),
			Credentials:      optionalMap(credentials),
		},
		Source: catalogSourceResponse(runtime.Source),
	}
}

func catalogCredentialInjectionResponse(injection catalog.CredentialInjection) api.CredentialInjection {
	environment := make(map[string]api.CredentialEnvironmentVariable)
	for name, variable := range injection.Environment {
		environment[name] = api.CredentialEnvironmentVariable{
			Key: variable.Key,
		}
	}

	files := make([]api.CredentialFile, 0, len(injection.Files))
	for _, file := range injection.Files {
		files = append(files, api.CredentialFile{Key: file.Key, Path: file.Path, Mode: optionalUint32(file.Mode)})
	}

	var directory *api.CredentialDirectory
	if injection.Directory != nil {
		directory = &api.CredentialDirectory{
			Path: injection.Directory.Path,
			Mode: optionalUint32(injection.Directory.Mode),
		}
	}

	return api.CredentialInjection{
		Environment:     optionalMap(environment),
		EnvironmentFrom: optionalBool(injection.EnvironmentFrom),
		Files:           optionalSlice(files),
		Directory:       directory,
	}
}

func catalogTargetResponse(
	target catalog.Target,
	state catalog.TargetState,
	commits map[repository.CommitKey]api.GitCommit,
	repositoryID uuid.UUID,
	archived bool,
	lastAttemptedSync *catalog.SyncRunCheckpoint,
	lastSuccessfulSync *catalog.SyncRunCheckpoint,
) (api.TargetDefinition, error) {
	actions, err := catalogActionsResponse(target.Spec.Actions)
	if err != nil {
		return api.TargetDefinition{}, err
	}

	return api.TargetDefinition{
		APIVersion: api.TargetDefinitionAPIVersion(target.APIVersion),
		Kind:       api.TargetDefinitionKind(target.Kind),
		Metadata:   catalogMetadataResponse(target.Metadata),
		Spec: api.TargetSpec{
			Path:        optionalString(target.Spec.Path),
			Watch:       optionalSlice(target.Spec.Watch),
			Runtime:     optionalString(target.Spec.Runtime),
			Placement:   optionalString(target.Spec.Placement),
			Credentials: optionalStringMap(target.Spec.Credentials),
			Timeout:     optionalString(target.Spec.Timeout),
			Resources:   catalogResourceRequirementsResponse(target.Spec.Resources),
			Actions:     actions,
		},
		Source: catalogSourceResponse(target.Source),
		Status: api.TargetStatus{
			DesiredCommit: commits[commitKey(repositoryID, state.DesiredCommitSHA)],
			Reconciliation: targetReconciliationStatusResponse(
				target,
				state.DesiredCommitSHA,
				archived,
				lastAttemptedSync,
				lastSuccessfulSync,
				commits,
				repositoryID,
			),
		},
	}, nil
}

func appendTargetCheckpointCommitKeys(
	keys []repository.CommitKey,
	repositoryID uuid.UUID,
	checkpoint *catalog.SyncRunCheckpoint,
) []repository.CommitKey {
	if checkpoint != nil && checkpoint.RelevanceCommitSHA != nil {
		return append(keys, commitKey(repositoryID, *checkpoint.RelevanceCommitSHA))
	}

	return keys
}

func targetReconciliationResponse(
	record catalog.TargetSummary,
	commits map[repository.CommitKey]api.GitCommit,
) api.TargetReconciliationStatus {
	return targetReconciliationStatusResponse(
		record.Target,
		record.DesiredCommitSHA,
		record.ArchivedAt != nil,
		record.LastAttemptedSync,
		record.LastSuccessfulSync,
		commits,
		record.RepositoryID,
	)
}

func targetReconciliationStatusResponse(
	target catalog.Target,
	desiredCommitSHA string,
	archived bool,
	lastAttemptedSync *catalog.SyncRunCheckpoint,
	lastSuccessfulSync *catalog.SyncRunCheckpoint,
	commits map[repository.CommitKey]api.GitCommit,
	repositoryID uuid.UUID,
) api.TargetReconciliationStatus {
	return api.TargetReconciliationStatus{
		Comparison: api.TargetReconciliationStatusComparison(catalog.CompareReconciliation(
			target,
			desiredCommitSHA,
			archived,
			lastSuccessfulSync,
		)),
		LastAttemptedSync:  targetSyncCheckpointResponse(lastAttemptedSync, commits, repositoryID),
		LastSuccessfulSync: targetSyncCheckpointResponse(lastSuccessfulSync, commits, repositoryID),
	}
}

func targetSyncCheckpointResponse(
	checkpoint *catalog.SyncRunCheckpoint,
	commits map[repository.CommitKey]api.GitCommit,
	repositoryID uuid.UUID,
) *api.TargetSyncRunCheckpoint {
	if checkpoint == nil {
		return nil
	}
	var outcome *api.RunOutcome
	if checkpoint.Outcome != nil {
		converted := api.RunOutcome(*checkpoint.Outcome)
		outcome = &converted
	}

	return &api.TargetSyncRunCheckpoint{
		RunID:                   checkpoint.RunID,
		RelevanceCommit:         optionalCommit(repositoryID, checkpoint.RelevanceCommitSHA, commits),
		Phase:                   api.RunPhase(checkpoint.Phase),
		Outcome:                 outcome,
		CreatedAt:               checkpoint.CreatedAt,
		AssignedAt:              checkpoint.AssignedAt,
		PreparingAt:             checkpoint.PreparingAt,
		StartedAt:               checkpoint.StartedAt,
		FinishedAt:              checkpoint.FinishedAt,
		CancellationRequestedAt: checkpoint.CancellationRequestedAt,
	}
}

func catalogActionsResponse(actions map[string]catalog.Action) (map[string]api.Action, error) {
	result := make(map[string]api.Action, len(actions))
	for name, action := range actions {
		triggers, err := catalogTriggersResponse(action.Triggers)
		if err != nil {
			return nil, fmt.Errorf("convert action %q triggers: %w", name, err)
		}

		result[name] = api.Action{
			Title:            optionalString(action.Title),
			Description:      optionalString(action.Description),
			DocumentationURL: optionalString(action.DocumentationURL),
			Intent:           optionalString(action.Intent),
			Command:          append([]string(nil), action.Command...),
			Runtime:          optionalString(action.Runtime),
			Placement:        optionalString(action.Placement),
			Credentials:      optionalStringMap(action.Credentials),
			Timeout:          optionalString(action.Timeout),
			Resources:        catalogResourceRequirementsResponse(action.Resources),
			Parameters:       catalogParametersResponse(action.Parameters),
			Artifacts:        catalogArtifactsResponse(action.Artifacts),
			Inputs:           catalogInputsResponse(action.Inputs),
			Triggers:         triggers,
		}
	}

	return result, nil
}

func catalogTriggersResponse(triggers []catalog.Trigger) (*[]api.ActionTrigger, error) {
	if len(triggers) == 0 {
		return nil, nil
	}

	result := make([]api.ActionTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		var parameters *map[string]api.ParameterValue
		var review *api.ActionTriggerReview
		if len(trigger.Parameters) > 0 {
			converted, err := runParameterResponse(trigger.Parameters)
			if err != nil {
				return nil, fmt.Errorf("convert trigger %q parameters: %w", trigger.Name, err)
			}
			parameters = &converted
		}
		if trigger.Review != "" {
			converted := api.ActionTriggerReview(trigger.Review)
			review = &converted
		}

		result = append(result, api.ActionTrigger{
			Name:       api.CatalogName(trigger.Name),
			Git:        api.GitTrigger{},
			Review:     review,
			Parameters: parameters,
		})
	}

	return &result, nil
}

func catalogResourceRequirementsResponse(resources catalog.ResourceRequirements) *api.ResourceRequirements {
	requests := optionalStringMap(resources.Requests)
	limits := optionalStringMap(resources.Limits)
	if requests == nil && limits == nil {
		return nil
	}

	return &api.ResourceRequirements{
		Requests: requests,
		Limits:   limits,
	}
}

func catalogParametersResponse(parameters map[string]catalog.Parameter) *map[string]api.Parameter {
	if len(parameters) == 0 {
		return nil
	}

	result := make(map[string]api.Parameter, len(parameters))
	for name, parameter := range parameters {
		var argument *api.ParameterArgument
		if parameter.Argument != nil {
			argument = &api.ParameterArgument{
				Name: optionalString(parameter.Argument.Name),
			}
		}

		result[name] = api.Parameter{
			Title:       optionalString(parameter.Title),
			Description: optionalString(parameter.Description),
			Type:        api.ParameterType(parameter.Type),
			Required:    optionalBool(parameter.Required),
			Values:      optionalSlice(parameter.Values),
			Environment: optionalString(parameter.Environment),
			Argument:    argument,
		}
	}

	return &result
}

func catalogArtifactsResponse(artifacts map[string]catalog.Artifact) *map[string]api.ArtifactDeclaration {
	if len(artifacts) == 0 {
		return nil
	}

	result := make(map[string]api.ArtifactDeclaration, len(artifacts))
	for name, artifact := range artifacts {
		result[name] = api.ArtifactDeclaration{
			Description: optionalString(artifact.Description),
			Path:        artifact.Path,
			MediaType:   optionalString(artifact.MediaType),
			Sensitive:   optionalBool(artifact.Sensitive),
		}
	}

	return &result
}

func catalogInputsResponse(inputs map[string]catalog.Input) *map[string]api.ArtifactInput {
	if len(inputs) == 0 {
		return nil
	}

	result := make(map[string]api.ArtifactInput, len(inputs))
	for name, input := range inputs {
		result[name] = api.ArtifactInput{
			Artifact: api.ArtifactInputSource{
				FromAction:    input.Artifact.FromAction,
				Compatibility: optionalCatalogCompatibility(input.Artifact.Compatibility),
			},
			MountPath: input.MountPath,
		}
	}

	return &result
}

func catalogMetadataResponse(metadata catalog.Metadata) api.ResourceMetadata {
	return api.ResourceMetadata{
		Name:  metadata.Name,
		Title: optionalString(metadata.Title),
	}
}

func catalogSourceResponse(source catalog.Source) api.CatalogSource {
	return api.CatalogSource{
		Path:     source.Path,
		Document: source.Document,
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func optionalBool(value bool) *bool {
	if !value {
		return nil
	}

	return &value
}

func optionalUint32(value uint32) *uint32 {
	if value == 0 {
		return nil
	}

	return &value
}

func optionalStringMap(value map[string]string) *map[string]string {
	if len(value) == 0 {
		return nil
	}

	cloned := maps.Clone(value)

	return &cloned
}

func optionalMap[K comparable, V any](value map[K]V) *map[K]V {
	if len(value) == 0 {
		return nil
	}

	return &value
}

func optionalSlice[T any](value []T) *[]T {
	if len(value) == 0 {
		return nil
	}

	cloned := append([]T(nil), value...)

	return &cloned
}

func optionalCatalogCompatibility(value string) *api.ArtifactInputSourceCompatibility {
	if value == "" {
		return nil
	}

	compatibility := api.ArtifactInputSourceCompatibility(value)

	return &compatibility
}
