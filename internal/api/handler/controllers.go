package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/controllercommand"
	"github.com/slipramp/slipramp/internal/controlleridentity"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

type ClusterService interface {
	ListClusters(ctx context.Context) ([]cluster.Status, error)
	RecordControllerContact(ctx context.Context, clusterName string) error
}

type ControllerCommandService interface {
	Next(context.Context, string) (controllercommand.Command, error)
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
	request api.ListClustersRequestObject,
) (api.ListClustersResponseObject, error) {
	limit, offset, windowErr := listWindow(request.Params.Limit, request.Params.Offset)
	if windowErr != nil {
		return api.ListClusters400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, windowErr.Error()),
		), nil
	}
	statuses, err := h.clusters.ListClusters(ctx)
	if err != nil {
		return nil, err
	}

	query := ""
	if request.Params.Q != nil {
		query = strings.ToLower(strings.TrimSpace(*request.Params.Q))
	}
	connection := ""
	if request.Params.Connection != nil {
		connection = string(*request.Params.Connection)
	}
	filtered := statuses[:0]
	for _, status := range statuses {
		if (query == "" || strings.Contains(strings.ToLower(status.Name), query)) &&
			(connection == "" || string(status.ConnectionState) == connection) {
			filtered = append(filtered, status)
		}
	}
	total := len(filtered)
	filtered = listPage(filtered, offset, limit)

	items := make([]api.Cluster, 0, len(filtered))
	for _, status := range filtered {
		items = append(items, api.Cluster{
			Name:            status.Name,
			ConnectionState: api.ClusterConnectionState(status.ConnectionState),
			FirstSeenAt:     status.FirstSeenAt,
			LastSeenAt:      status.LastSeenAt,
		})
	}

	return api.ListClusters200JSONResponse{Items: items, Total: total, Limit: limit, Offset: offset}, nil
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

	command, err := h.controllerCommands.Next(ctx, principal.ClusterName)
	if err != nil {
		return nil, err
	}
	if command != nil {
		body, err := h.controllerCommandResponse(command)
		if err != nil {
			return nil, err
		}

		return api.PollForControllerCommand200JSONResponse(body), nil
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

	command, err = h.controllerCommands.Next(ctx, principal.ClusterName)
	if err != nil {
		return nil, err
	}
	if command == nil {
		return api.PollForControllerCommand204Response{}, nil
	}

	body, err := h.controllerCommandResponse(command)
	if err != nil {
		return nil, err
	}

	return api.PollForControllerCommand200JSONResponse(body), nil
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
			Images:      runObservationImages(request.Body.Images),
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

func (h *Handler) ReportPlacementPreflight(
	ctx context.Context,
	request api.ReportPlacementPreflightRequestObject,
) (api.ReportPlacementPreflightResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		return api.ReportPlacementPreflight400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, "A Placement preflight report is required."),
		), nil
	}

	diagnostics := make([]placement.PreflightDiagnostic, 0)
	if request.Body.Diagnostics != nil {
		for _, diagnostic := range *request.Body.Diagnostics {
			diagnostics = append(diagnostics, placement.PreflightDiagnostic{
				Reason:          diagnostic.Reason,
				Message:         diagnostic.Message,
				Count:           diagnostic.Count,
				FirstObservedAt: diagnostic.FirstObservedAt,
				LastObservedAt:  diagnostic.LastObservedAt,
			})
		}
	}

	_, err = h.placementPreflights.Report(ctx, placement.PreflightReport{
		ID:           request.PreflightID,
		ClusterName:  principal.ClusterName,
		State:        placement.PreflightState(request.Body.State),
		CleanupState: placement.PreflightCleanupState(request.Body.CleanupState),
		JobUID:       request.Body.JobUID,
		Reason:       request.Body.Reason,
		Message:      request.Body.Message,
		Diagnostics:  diagnostics,
		ObservedAt:   h.now().UTC(),
	})
	if err != nil {
		switch {
		case errors.Is(err, placement.ErrPreflightInvalid):
			return api.ReportPlacementPreflight400ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusBadRequest, err.Error()),
			), nil
		case errors.Is(err, placement.ErrPreflightNotFound):
			return api.ReportPlacementPreflight404ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusNotFound, "The preflight is not assigned to this Controller's Cluster."),
			), nil
		case errors.Is(err, placement.ErrPreflightConflict):
			return api.ReportPlacementPreflight409ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusConflict, "The report conflicts with the durable preflight result."),
			), nil
		default:
			return nil, err
		}
	}
	if err := h.clusters.RecordControllerContact(ctx, principal.ClusterName); err != nil {
		return nil, err
	}

	return api.ReportPlacementPreflight204Response{}, nil
}

func (h *Handler) ReportKargoPromotionDispatch(
	ctx context.Context,
	request api.ReportKargoPromotionDispatchRequestObject,
) (api.ReportKargoPromotionDispatchResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A Kargo Promotion dispatch report is required.")

		return api.ReportKargoPromotionDispatch400ApplicationProblemPlusJSONResponse(problem), nil
	}

	err = h.promotionDispatches.Report(
		ctx,
		principal.ClusterName,
		promotion.CreationDispatchReport{
			PromotionID: request.PromotionID,
			Attempt:     request.Body.Attempt,
			Result:      promotion.CreationDispatchResult(request.Body.Result),
			KargoName:   request.Body.KargoName,
			KargoUID:    request.Body.KargoUID,
			Diagnostic:  request.Body.Diagnostic,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, promotion.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.ReportKargoPromotionDispatch400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, promotion.ErrPromotionNotFound):
			problem := NewProblem(http.StatusNotFound, "The Promotion is not assigned to this controller's cluster.")

			return api.ReportKargoPromotionDispatch404ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, promotion.ErrCreationDispatchConflict):
			problem := NewProblem(http.StatusConflict, "The dispatch report conflicts with the Kargo Promotion identity.")

			return api.ReportKargoPromotionDispatch409ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}
	if err := h.clusters.RecordControllerContact(ctx, principal.ClusterName); err != nil {
		return nil, err
	}

	return api.ReportKargoPromotionDispatch204Response{}, nil
}

func (h *Handler) ReportKargoPromotionReconciliation(
	ctx context.Context,
	request api.ReportKargoPromotionReconciliationRequestObject,
) (api.ReportKargoPromotionReconciliationResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A Kargo Promotion reconciliation report is required.")

		return api.ReportKargoPromotionReconciliation400ApplicationProblemPlusJSONResponse(problem), nil
	}

	err = h.promotionReconciliations.Report(
		ctx,
		principal.ClusterName,
		promotion.ReconciliationReport{
			PromotionID: request.PromotionID,
			KargoName:   request.Body.KargoName,
			KargoUID:    request.Body.KargoUID,
			Phase:       optionalKargoPhase(request.Body.Phase),
			Diagnostic:  request.Body.Diagnostic,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, promotion.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.ReportKargoPromotionReconciliation400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, promotion.ErrPromotionNotFound):
			problem := NewProblem(http.StatusNotFound, "The Promotion is not assigned to this controller's cluster.")

			return api.ReportKargoPromotionReconciliation404ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, promotion.ErrReconciliationConflict):
			problem := NewProblem(
				http.StatusConflict,
				"The reconciliation report conflicts with the canonical Kargo Promotion identity.",
			)

			return api.ReportKargoPromotionReconciliation409ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}
	if err := h.clusters.RecordControllerContact(ctx, principal.ClusterName); err != nil {
		return nil, err
	}

	return api.ReportKargoPromotionReconciliation204Response{}, nil
}

func runObservationOutcome(value *api.RunObservationOutcome) *run.Outcome {
	if value == nil {
		return nil
	}

	converted := run.Outcome(*value)

	return &converted
}

func runObservationImages(value *api.ExecutionImages) run.ExecutionImages {
	if value == nil {
		return run.ExecutionImages{}
	}

	return run.ExecutionImages{
		Action:   value.Action,
		Checkout: value.Checkout,
		Harness:  value.Harness,
	}
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

func createKargoPromotionCommand(command promotion.CreationCommand) api.ControllerCommand {
	return api.ControllerCommand{
		Type: api.ControllerCommandTypeCreateKargoPromotion,
		Promotion: &api.KargoPromotionCreationCommand{
			ID:           command.PromotionID,
			Attempt:      command.Attempt,
			ExternalName: command.ExternalName,
			Integration:  api.CatalogName(command.Integration),
			Project:      api.CatalogName(command.Project),
			Stage:        command.Stage,
			StageUID:     command.StageUID,
			Freight:      command.Freight,
		},
	}
}

func reconcileKargoPromotionCommand(command promotion.ReconciliationCommand) api.ControllerCommand {
	return api.ControllerCommand{
		Type: api.ControllerCommandTypeReconcileKargoPromotion,
		PromotionReconciliation: &api.KargoPromotionReconciliationCommand{
			ID:          command.PromotionID,
			Source:      api.PromotionSource(command.Source),
			Integration: api.CatalogName(command.Integration),
			Project:     api.CatalogName(command.Project),
			Stage:       command.Stage,
			KargoName:   command.KargoName,
			KargoUID:    command.KargoUID,
		},
	}
}

func placementPreflightCommand(record placement.Preflight) (api.ControllerCommand, error) {
	podTemplate, err := assignmentPodTemplateResponse(record.Snapshot.PodTemplate)
	if err != nil {
		return api.ControllerCommand{}, err
	}
	resources := api.ResourceRequirements{
		Requests: optionalStringMap(record.Snapshot.Resources.Requests),
		Limits:   optionalStringMap(record.Snapshot.Resources.Limits),
	}
	return api.ControllerCommand{
		Type: api.ControllerCommandTypePlacementPreflight,
		PlacementPreflight: &api.PlacementPreflightCommand{
			ID:                    record.ID,
			PlacementName:         api.CatalogName(record.Snapshot.PlacementName),
			ConfigurationChecksum: record.Snapshot.ConfigurationChecksum,
			Cluster:               api.CatalogName(record.Snapshot.ClusterName),
			JobNamespace:          api.CatalogName(record.Snapshot.JobNamespace),
			ServiceAccountName:    optionalString(record.Snapshot.ServiceAccountName),
			TimeoutSeconds:        int32(placement.DefaultPreflightTimeout.Seconds()),
			Resources:             &resources,
			NodeSelector:          optionalStringMap(record.Snapshot.NodeSelector),
			PodTemplate:           podTemplate,
		},
	}, nil
}

func optionalKargoPhase(value *api.KargoPromotionReconciliationReportPhase) *promotion.KargoPhase {
	if value == nil {
		return nil
	}

	phase := promotion.KargoPhase(*value)
	return &phase
}

func (h *Handler) controllerCommandResponse(command controllercommand.Command) (api.ControllerCommand, error) {
	switch selected := command.(type) {
	case controllercommand.Cancellation:
		return cancelRunCommand(selected.Run), nil
	case controllercommand.PromotionCreation:
		return createKargoPromotionCommand(selected.Command), nil
	case controllercommand.PromotionReconciliation:
		return reconcileKargoPromotionCommand(selected.Command), nil
	case controllercommand.PlacementPreflight:
		return placementPreflightCommand(selected.Preflight)
	case controllercommand.Assignment:
		assignment, err := h.runAssignmentResponse(selected.Run)
		if err != nil {
			return api.ControllerCommand{}, err
		}

		return runAssignmentCommand(assignment), nil
	default:
		return api.ControllerCommand{}, fmt.Errorf("unsupported Controller command %T", command)
	}
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
