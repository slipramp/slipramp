package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/run"
)

type ClusterService interface {
	ListClusters(ctx context.Context) ([]cluster.Status, error)
	RecordControllerContact(ctx context.Context, clusterName string) error
}

const (
	defaultControllerPollWaitSeconds = 25
	maximumControllerPollWaitSeconds = 30
)

func (h *Handler) EnrollController(
	ctx context.Context,
	request api.EnrollControllerRequestObject,
) (api.EnrollControllerResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "An enrollment request is required.")

		return api.EnrollController400ApplicationProblemPlusJSONResponse(problem), nil
	}

	err := h.identities.EnrollController(ctx, request.Body.ClusterName, request.Body.CredentialSha256)
	if err != nil {
		switch {
		case errors.Is(err, controlleridentity.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.EnrollController400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, controlleridentity.ErrEnrollmentConflict):
			problem := NewProblem(http.StatusConflict, "The cluster is already enrolled with another credential.")

			return api.EnrollController409ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}

	return api.EnrollController204Response{}, nil
}

func (h *Handler) ListClusters(
	ctx context.Context,
	_ api.ListClustersRequestObject,
) (api.ListClustersResponseObject, error) {
	statuses, err := h.clusters.ListClusters(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]api.Cluster, 0, len(statuses))
	for _, status := range statuses {
		items = append(items, api.Cluster{
			Name:            status.Name,
			ConnectionState: api.ClusterConnectionState(status.ConnectionState),
			FirstSeenAt:     status.FirstSeenAt,
			LastSeenAt:      status.LastSeenAt,
		})
	}

	return api.ListClusters200JSONResponse{Items: items}, nil
}

func (h *Handler) PollForControllerCommand(
	ctx context.Context,
	request api.PollForControllerCommandRequestObject,
) (api.PollForControllerCommandResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}

	timeoutSeconds := defaultControllerPollWaitSeconds
	if request.Params.TimeoutSeconds != nil {
		timeoutSeconds = *request.Params.TimeoutSeconds
	}

	if timeoutSeconds < 0 || timeoutSeconds > maximumControllerPollWaitSeconds {
		problem := NewProblem(http.StatusBadRequest, "timeoutSeconds must be between 0 and 30")

		return api.PollForControllerCommand400ApplicationProblemPlusJSONResponse(problem), nil
	}

	if err := h.clusters.RecordControllerContact(ctx, principal.ClusterName); err != nil {
		return nil, err
	}

	command, err := h.nextControllerCommand(ctx, principal.ClusterName)
	if err != nil {
		return nil, err
	}
	if command != nil {
		return api.PollForControllerCommand200JSONResponse(*command), nil
	}

	if timeoutSeconds == 0 {
		return api.PollForControllerCommand204Response{}, nil
	}

	timer := time.NewTimer(time.Duration(timeoutSeconds) * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}

	command, err = h.nextControllerCommand(ctx, principal.ClusterName)
	if err != nil {
		return nil, err
	}
	if command == nil {
		return api.PollForControllerCommand204Response{}, nil
	}

	return api.PollForControllerCommand200JSONResponse(*command), nil
}

func (h *Handler) ReportRunObservation(
	ctx context.Context,
	request api.ReportRunObservationRequestObject,
) (api.ReportRunObservationResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}

	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A Run observation is required.")

		return api.ReportRunObservation400ApplicationProblemPlusJSONResponse(problem), nil
	}

	err = h.runs.ReportObservation(
		ctx,
		run.Observation{
			ID:          request.RunID,
			ClusterName: principal.ClusterName,
			Phase:       run.Phase(request.Body.Phase),
			Outcome:     runObservationOutcome(request.Body.Outcome),
			JobUID:      request.Body.JobUID,
			Reason:      request.Body.Reason,
			Message:     request.Body.Message,
			ExitCode:    request.Body.ExitCode,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, run.ErrInvalidRequest):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.ReportRunObservation400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, run.ErrRunNotFound):
			problem := NewProblem(http.StatusNotFound, "The Run is not assigned to this controller's cluster.")

			return api.ReportRunObservation404ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, run.ErrObservationConflict):
			problem := NewProblem(http.StatusConflict, "The observation conflicts with the Run or its Kubernetes Job.")

			return api.ReportRunObservation409ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}
	if err := h.clusters.RecordControllerContact(ctx, principal.ClusterName); err != nil {
		return nil, err
	}

	return api.ReportRunObservation204Response{}, nil
}

func runObservationOutcome(value *api.RunObservationOutcome) *run.Outcome {
	if value == nil {
		return nil
	}

	converted := run.Outcome(*value)

	return &converted
}

func runAssignmentCommand(assignment api.RunAssignment) api.ControllerCommand {
	return api.ControllerCommand{
		Type:       api.ControllerCommandTypeRunAssignment,
		Assignment: &assignment,
	}
}

func cancelRunCommand(record run.Run) api.ControllerCommand {
	return api.ControllerCommand{
		Type: api.ControllerCommandTypeCancelRun,
		Cancel: &api.CancelRunCommand{
			ID:           record.ID,
			JobNamespace: api.CatalogName(record.Snapshot.Placement.JobNamespace),
			JobUID:       record.JobUID,
		},
	}
}

func (h *Handler) nextControllerCommand(ctx context.Context, clusterName string) (*api.ControllerCommand, error) {
	record, err := h.runs.NextRunToCancel(ctx, clusterName)
	if err == nil {
		command := cancelRunCommand(record)

		return &command, nil
	}
	if !errors.Is(err, run.ErrNoRunToCancel) {
		return nil, err
	}

	assignment, err := h.nextRunAssignment(ctx, clusterName)
	if errors.Is(err, run.ErrNoRunAssignment) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	command := runAssignmentCommand(assignment)

	return &command, nil
}

func (h *Handler) nextRunAssignment(ctx context.Context, clusterName string) (api.RunAssignment, error) {
	record, err := h.runs.NextRunAssignment(ctx, clusterName)
	if err != nil {
		return api.RunAssignment{}, err
	}

	return h.runAssignmentResponse(record)
}

func (h *Handler) runAssignmentResponse(record run.Run) (api.RunAssignment, error) {
	parameters, err := runParameterResponse(record.Parameters)
	if err != nil {
		return api.RunAssignment{}, err
	}
	token := h.artifacts.RunToken(record.ID)

	podTemplate, err := assignmentPodTemplateResponse(record.Snapshot.Placement.PodTemplate)
	if err != nil {
		return api.RunAssignment{}, err
	}

	return api.RunAssignment{
		ID:                record.ID,
		CatalogRevisionID: record.CatalogRevisionID,
		CommitSha:         record.CommitSHA,
		Parameters:        parameters,
		Repository:        assignmentRepositoryResponse(record.Snapshot.Repository),
		Target: api.RunAssignmentTarget{
			Name: api.CatalogName(record.Snapshot.Target.Metadata.Name),
			Path: optionalString(record.Snapshot.Target.Spec.Path),
		},
		Action:    assignmentActionResponse(record.Snapshot.Action),
		Runtime:   assignmentRuntimeResponse(record.Snapshot.Runtime),
		Placement: assignmentPlacementResponse(record.Snapshot.Placement, podTemplate),
		ArtifactAccess: api.RunArtifactAccess{
			Token: token,
		},
	}, nil
}

func assignmentRepositoryResponse(repository run.RepositorySnapshot) api.RunAssignmentRepository {
	var checkoutCredentialName *api.CatalogName
	if repository.CheckoutCredentialName != nil {
		name := api.CatalogName(*repository.CheckoutCredentialName)
		checkoutCredentialName = &name
	}

	return api.RunAssignmentRepository{
		Name:                   api.CatalogName(repository.Name),
		CloneURL:               repository.CloneURL,
		Branch:                 repository.Branch,
		CheckoutCredentialName: checkoutCredentialName,
	}
}

func assignmentActionResponse(action catalog.EffectiveAction) api.RunAssignmentAction {
	credentials := make(map[string]api.CatalogName, len(action.Credentials))
	for name, credential := range action.Credentials {
		credentials[name] = api.CatalogName(credential)
	}

	return api.RunAssignmentAction{
		Name:                 api.CatalogName(action.Name),
		Intent:               optionalString(action.Intent),
		Command:              append([]string(nil), action.Command...),
		Runtime:              api.CatalogName(action.Runtime),
		Placement:            api.CatalogName(action.Placement),
		Credentials:          optionalMap(credentials),
		Timeout:              optionalString(action.Timeout),
		Resources:            catalogResourceRequirementsResponse(action.Resources),
		ParameterDefinitions: catalogParametersResponse(action.Parameters),
		Artifacts:            catalogArtifactsResponse(action.Artifacts),
		Inputs:               catalogInputsResponse(action.Inputs),
	}
}

func assignmentRuntimeResponse(runtime catalog.Runtime) api.RunAssignmentRuntime {
	credentials := make(map[string]api.RuntimeCredential, len(runtime.Spec.Credentials))
	for name, credential := range runtime.Spec.Credentials {
		credentials[name] = api.RuntimeCredential{
			Inject: catalogCredentialInjectionResponse(credential.Inject),
		}
	}

	return api.RunAssignmentRuntime{
		Name:             api.CatalogName(runtime.Metadata.Name),
		Image:            runtime.Spec.Image,
		WorkingDirectory: optionalString(runtime.Spec.WorkingDirectory),
		Environment:      optionalStringMap(runtime.Spec.Environment),
		Credentials:      optionalMap(credentials),
	}
}

func assignmentPlacementResponse(
	placement run.PlacementSnapshot,
	podTemplate *map[string]any,
) api.RunAssignmentPlacement {
	credentials := make(map[string]api.RunAssignmentCredential, len(placement.Credentials))
	for name, credential := range placement.Credentials {
		credentials[name] = api.RunAssignmentCredential{
			SecretName: credential.SecretName,
		}
	}

	return api.RunAssignmentPlacement{
		Name:               api.CatalogName(placement.Name),
		Cluster:            api.CatalogName(placement.ClusterName),
		JobNamespace:       api.CatalogName(placement.JobNamespace),
		ServiceAccountName: optionalString(placement.ServiceAccountName),
		Timeout:            optionalString(placement.Timeout),
		Resources:          catalogResourceRequirementsResponse(placement.Resources),
		NodeSelector:       optionalStringMap(placement.NodeSelector),
		Credentials:        optionalMap(credentials),
		PodTemplate:        podTemplate,
	}
}

func assignmentPodTemplateResponse(podTemplate *corev1.PodTemplateSpec) (*map[string]any, error) {
	if podTemplate == nil {
		return nil, nil
	}

	encoded, err := json.Marshal(podTemplate)
	if err != nil {
		return nil, fmt.Errorf("encode run assignment pod template: %w", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, fmt.Errorf("decode run assignment pod template: %w", err)
	}

	return &decoded, nil
}
