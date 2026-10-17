package handler

import (
	"context"
	"errors"
	"maps"
	"net/http"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
)

type CatalogService interface {
	GetActiveRevision(
		ctx context.Context,
		repositoryID uuid.UUID,
	) (catalog.Revision, error)
}

func (h *Handler) GetRepositoryCatalog(
	ctx context.Context,
	request api.GetRepositoryCatalogRequestObject,
) (api.GetRepositoryCatalogResponseObject, error) {
	revision, err := h.catalogs.GetActiveRevision(
		ctx,
		request.RepositoryID,
	)
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

	return api.GetRepositoryCatalog200JSONResponse(
		catalogRevisionResponse(revision),
	), nil
}

func catalogRevisionResponse(revision catalog.Revision) api.CatalogRevision {
	runtimes := make([]api.Runtime, 0, len(revision.Catalog.Runtimes))
	for _, runtime := range revision.Catalog.Runtimes {
		runtimes = append(runtimes, catalogRuntimeResponse(runtime))
	}

	targets := make([]api.Target, 0, len(revision.Catalog.Targets))
	for _, target := range revision.Catalog.Targets {
		targets = append(targets, catalogTargetResponse(target))
	}

	return api.CatalogRevision{
		ID:        revision.ID,
		CommitSha: revision.CommitSHA,
		CreatedAt: revision.CreatedAt,
		Runtimes:  runtimes,
		Targets:   targets,
	}
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

func catalogCredentialInjectionResponse(
	injection catalog.CredentialInjection,
) api.CredentialInjection {
	environment := make(
		map[string]api.CredentialEnvironmentVariable,
	)
	for name, variable := range injection.Environment {
		environment[name] = api.CredentialEnvironmentVariable{
			Key: variable.Key,
		}
	}

	files := make([]api.CredentialFile, 0, len(injection.Files))
	for _, file := range injection.Files {
		files = append(files, api.CredentialFile{
			Key:  file.Key,
			Path: file.Path,
			Mode: optionalUint32(file.Mode),
		})
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

func catalogTargetResponse(target catalog.Target) api.Target {
	return api.Target{
		APIVersion: api.TargetAPIVersion(target.APIVersion),
		Kind:       api.TargetKind(target.Kind),
		Metadata:   catalogMetadataResponse(target.Metadata),
		Spec: api.TargetSpec{
			Path:        optionalString(target.Spec.Path),
			Watch:       optionalSlice(target.Spec.Watch),
			Runtime:     optionalString(target.Spec.Runtime),
			Placement:   optionalString(target.Spec.Placement),
			Credentials: optionalStringMap(target.Spec.Credentials),
			Timeout:     optionalString(target.Spec.Timeout),
			Resources: catalogResourceRequirementsResponse(
				target.Spec.Resources,
			),
			Actions: catalogActionsResponse(target.Spec.Actions),
		},
		Source: catalogSourceResponse(target.Source),
	}
}

func catalogActionsResponse(
	actions map[string]catalog.Action,
) map[string]api.Action {
	result := make(map[string]api.Action, len(actions))
	for name, action := range actions {
		result[name] = api.Action{
			Intent:      optionalString(action.Intent),
			Command:     append([]string(nil), action.Command...),
			Runtime:     optionalString(action.Runtime),
			Placement:   optionalString(action.Placement),
			Credentials: optionalStringMap(action.Credentials),
			Timeout:     optionalString(action.Timeout),
			Resources: catalogResourceRequirementsResponse(
				action.Resources,
			),
			Parameters: catalogParametersResponse(action.Parameters),
			Artifacts:  catalogArtifactsResponse(action.Artifacts),
			Inputs:     catalogInputsResponse(action.Inputs),
		}
	}

	return result
}

func catalogResourceRequirementsResponse(
	resources catalog.ResourceRequirements,
) *api.ResourceRequirements {
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

func catalogParametersResponse(
	parameters map[string]catalog.Parameter,
) *map[string]api.Parameter {
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
			Type:        api.ParameterType(parameter.Type),
			Required:    optionalBool(parameter.Required),
			Values:      optionalSlice(parameter.Values),
			Environment: optionalString(parameter.Environment),
			Argument:    argument,
		}
	}

	return &result
}

func catalogArtifactsResponse(
	artifacts map[string]catalog.Artifact,
) *map[string]api.ArtifactDeclaration {
	if len(artifacts) == 0 {
		return nil
	}

	result := make(map[string]api.ArtifactDeclaration, len(artifacts))
	for name, artifact := range artifacts {
		result[name] = api.ArtifactDeclaration{
			Path:      artifact.Path,
			MediaType: optionalString(artifact.MediaType),
			Sensitive: optionalBool(artifact.Sensitive),
		}
	}

	return &result
}

func catalogInputsResponse(
	inputs map[string]catalog.Input,
) *map[string]api.ArtifactInput {
	if len(inputs) == 0 {
		return nil
	}

	result := make(map[string]api.ArtifactInput, len(inputs))
	for name, input := range inputs {
		result[name] = api.ArtifactInput{
			Artifact: api.ArtifactInputSource{
				FromAction: input.Artifact.FromAction,
				Compatibility: optionalCatalogCompatibility(
					input.Artifact.Compatibility,
				),
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

func optionalCatalogCompatibility(
	value string,
) *api.ArtifactInputSourceCompatibility {
	if value == "" {
		return nil
	}

	compatibility := api.ArtifactInputSourceCompatibility(value)

	return &compatibility
}
