package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/promotion/projection"
)

type PromotionProjectService interface {
	List(context.Context, projection.ProjectListFilter) (projection.ProjectList, error)
	ListPipelines(context.Context, projection.PipelineListFilter) (projection.PipelineList, error)
	ListStages(context.Context, projection.StageListFilter) (projection.StageList, error)
	Get(context.Context, projection.ProjectKey) (projection.ProjectDetail, error)
	GetStage(context.Context, uuid.UUID) (projection.StageDetail, error)
}

func (h *Handler) ListPromotionPipelines(
	ctx context.Context,
	request api.ListPromotionPipelinesRequestObject,
) (api.ListPromotionPipelinesResponseObject, error) {
	filter := projection.PipelineListFilter{
		Integration: optionalCatalogName(request.Params.Integration),
		Cluster:     optionalCatalogName(request.Params.Cluster),
		Project:     optionalCatalogName(request.Params.Project),
		Limit:       promotion.DefaultListLimit,
	}
	if request.Params.FreightAlignment != nil {
		filter.FreightAlignment = projection.FreightAlignmentStatus(*request.Params.FreightAlignment)
	}
	filter.Unhealthy = request.Params.Unhealthy != nil && *request.Params.Unhealthy
	filter.Stale = request.Params.Stale != nil && *request.Params.Stale
	filter.PendingReview = request.Params.PendingReview != nil && *request.Params.PendingReview
	filter.NewerFreight = request.Params.NewerFreight != nil && *request.Params.NewerFreight
	if request.Params.Limit != nil {
		filter.Limit = int(*request.Params.Limit)
	}
	if request.Params.Offset != nil {
		filter.Offset = int(*request.Params.Offset)
	}

	result, err := h.promotionProjects.ListPipelines(ctx, filter)
	if errors.Is(err, promotion.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListPromotionPipelines400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	items := make([]api.PromotionPipeline, 0, len(result.Items))
	for _, pipeline := range result.Items {
		stages := make([]api.PromotionProjectStage, 0, len(pipeline.Stages))
		for _, stage := range pipeline.Stages {
			stages = append(stages, h.promotionProjectStageResponse(stage))
		}
		items = append(items, api.PromotionPipeline{
			Integration: api.CatalogName(pipeline.Integration),
			Cluster:     api.CatalogName(pipeline.ClusterName),
			Project:     api.CatalogName(pipeline.Project),
			Name:        pipeline.Name,
			Stages:      stages,
		})
	}

	return api.ListPromotionPipelines200JSONResponse{
		Items: items, Total: result.Total, Limit: result.Limit, Offset: result.Offset,
	}, nil
}

func (h *Handler) ListPromotionProjects(
	ctx context.Context,
	request api.ListPromotionProjectsRequestObject,
) (api.ListPromotionProjectsResponseObject, error) {
	filter := projection.ProjectListFilter{
		Integration: optionalCatalogName(request.Params.Integration),
		Cluster:     optionalCatalogName(request.Params.Cluster),
		Project:     optionalCatalogName(request.Params.Project),
		Limit:       promotion.DefaultListLimit,
	}
	if request.Params.Limit != nil {
		filter.Limit = int(*request.Params.Limit)
	}
	if request.Params.Offset != nil {
		filter.Offset = int(*request.Params.Offset)
	}
	if request.Params.FreightAlignment != nil {
		filter.FreightAlignment = projection.FreightAlignmentStatus(*request.Params.FreightAlignment)
	}
	filter.Unhealthy = request.Params.Unhealthy != nil && *request.Params.Unhealthy
	filter.Stale = request.Params.Stale != nil && *request.Params.Stale
	filter.PendingReview = request.Params.PendingReview != nil && *request.Params.PendingReview

	result, err := h.promotionProjects.List(ctx, filter)
	if errors.Is(err, promotion.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListPromotionProjects400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	items := make([]api.PromotionProject, 0, len(result.Items))
	for _, project := range result.Items {
		items = append(items, api.PromotionProject{
			Integration:                   api.CatalogName(project.Integration),
			Cluster:                       api.CatalogName(project.ClusterName),
			Project:                       api.CatalogName(project.Project),
			StageCount:                    project.StageCount,
			UnhealthyStageCount:           project.UnhealthyStageCount,
			StaleStageCount:               project.StaleStageCount,
			PendingReviewCount:            project.PendingReviewCount,
			FreightDifferenceCount:        project.FreightDifferenceCount,
			UnknownFreightComparisonCount: project.UnknownFreightComparisonCount,
			LatestObservedAt:              project.LatestObservedAt,
			KargoURL: h.kargoIntegrations.KargoProjectURL(
				project.Integration,
				project.Project,
			),
		})
	}

	return api.ListPromotionProjects200JSONResponse{
		Items: items,
		Summary: api.PromotionProjectListSummary{
			StageCount:                    result.Summary.StageCount,
			UnhealthyStageCount:           result.Summary.UnhealthyStageCount,
			StaleStageCount:               result.Summary.StaleStageCount,
			PendingReviewCount:            result.Summary.PendingReviewCount,
			FreightDifferenceCount:        result.Summary.FreightDifferenceCount,
			UnknownFreightComparisonCount: result.Summary.UnknownFreightComparisonCount,
		},
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
}

func (h *Handler) ListPromotionStages(
	ctx context.Context,
	request api.ListPromotionStagesRequestObject,
) (api.ListPromotionStagesResponseObject, error) {
	filter := projection.StageListFilter{
		Integration: optionalCatalogName(request.Params.Integration),
		Cluster:     optionalCatalogName(request.Params.Cluster),
		Project:     optionalCatalogName(request.Params.Project),
		Limit:       promotion.DefaultListLimit,
	}
	if request.Params.Application != nil {
		filter.Application = *request.Params.Application
	}
	if request.Params.FreightAlignment != nil {
		filter.FreightAlignment = projection.FreightAlignmentStatus(*request.Params.FreightAlignment)
	}
	filter.Unhealthy = request.Params.Unhealthy != nil && *request.Params.Unhealthy
	filter.Stale = request.Params.Stale != nil && *request.Params.Stale
	filter.PendingReview = request.Params.PendingReview != nil && *request.Params.PendingReview
	filter.NewerFreight = request.Params.NewerFreight != nil && *request.Params.NewerFreight
	if request.Params.Limit != nil {
		filter.Limit = int(*request.Params.Limit)
	}
	if request.Params.Offset != nil {
		filter.Offset = int(*request.Params.Offset)
	}

	result, err := h.promotionProjects.ListStages(ctx, filter)
	if errors.Is(err, promotion.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListPromotionStages400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	items := make([]api.PromotionStageDetail, 0, len(result.Items))
	for _, stage := range result.Items {
		items = append(items, h.promotionStageDetailResponse(stage))
	}

	return api.ListPromotionStages200JSONResponse{
		Items: items, Total: result.Total, Limit: result.Limit, Offset: result.Offset,
	}, nil
}

func (h *Handler) GetPromotionProject(
	ctx context.Context,
	request api.GetPromotionProjectRequestObject,
) (api.GetPromotionProjectResponseObject, error) {
	result, err := h.promotionProjects.Get(ctx, projection.ProjectKey{
		Integration: string(request.Integration), Cluster: string(request.Cluster), Project: string(request.Project),
	})
	if errors.Is(err, promotion.ErrProjectNotFound) {
		problem := NewProblem(http.StatusNotFound, "The observed Kargo Project does not exist.")

		return api.GetPromotionProject404ApplicationProblemPlusJSONResponse{
			NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	stages := make([]api.PromotionProjectStage, 0, len(result.Stages))
	for _, stage := range result.Stages {
		stages = append(stages, h.promotionProjectStageResponse(stage))
	}

	return api.GetPromotionProject200JSONResponse{
		Integration: api.CatalogName(result.Integration),
		Cluster:     api.CatalogName(result.ClusterName),
		Project:     api.CatalogName(result.Project),
		KargoURL:    h.kargoIntegrations.KargoProjectURL(result.Integration, result.Project),
		Stages:      stages,
	}, nil
}

func (h *Handler) GetPromotionStage(
	ctx context.Context,
	request api.GetPromotionStageRequestObject,
) (api.GetPromotionStageResponseObject, error) {
	result, err := h.promotionProjects.GetStage(ctx, request.StageID)
	if errors.Is(err, promotion.ErrStageNotFound) {
		problem := NewProblem(http.StatusNotFound, "The observed Kargo Stage does not exist.")

		return api.GetPromotionStage404ApplicationProblemPlusJSONResponse{
			NotFoundApplicationProblemPlusJSONResponse: api.NotFoundApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	return api.GetPromotionStage200JSONResponse(h.promotionStageDetailResponse(result)), nil
}

func (h *Handler) promotionStageDetailResponse(result projection.StageDetail) api.PromotionStageDetail {
	return api.PromotionStageDetail{
		Integration: api.CatalogName(result.Integration),
		Cluster:     api.CatalogName(result.ClusterName),
		Project:     api.CatalogName(result.Project),
		Stage:       h.promotionProjectStageResponse(result.Stage),
	}
}

func (h *Handler) promotionProjectStageResponse(value projection.ProjectStage) api.PromotionProjectStage {
	stage := value.Stage
	conditions := make([]api.PromotionStageCondition, 0, len(stage.Conditions))
	for _, condition := range stage.Conditions {
		conditions = append(conditions, api.PromotionStageCondition{
			Type: condition.Type, Status: condition.Status, Reason: optionalString(condition.Reason),
			Message: optionalString(condition.Message), LastTransitionTime: condition.LastTransitionTime,
		})
	}
	requests := make([]api.PromotionProjectFreightRequest, 0, len(value.RequestedFreight))
	for _, request := range value.RequestedFreight {
		sources := make([]api.PromotionProjectStageSource, 0, len(request.Sources))
		for _, source := range request.Sources {
			sources = append(sources, api.PromotionProjectStageSource{Name: source.Name, StageID: source.StageID})
		}
		candidates := make([]api.PromotionFreightCandidate, 0, len(request.Candidates))
		for _, candidate := range request.Candidates {
			candidates = append(candidates, api.PromotionFreightCandidate{
				Freight:   promotionFreightItemResponse(candidate.Freight),
				Readiness: api.PromotionFreightCandidateReadiness(candidate.Evidence.Readiness),
				Reason:    optionalString(candidate.Evidence.Reason),
				Evidence:  append([]string{}, candidate.Evidence.Facts...),
			})
		}
		var requiredSoakTime *string
		if request.Request.RequiredSoakTime > 0 {
			formatted := request.Request.RequiredSoakTime.String()
			requiredSoakTime = &formatted
		}
		requests = append(requests, api.PromotionProjectFreightRequest{
			Origin: request.Request.Origin, Direct: request.Request.Direct, Sources: sources,
			AvailabilityStrategy: api.PromotionProjectFreightRequestAvailabilityStrategy(
				request.Request.AvailabilityStrategy.Effective(),
			),
			RequiredSoakTime: requiredSoakTime, Candidates: candidates,
		})
	}
	alignments := make([]api.PromotionFreightAlignment, 0, len(value.FreightAlignments))
	for _, alignment := range value.FreightAlignments {
		alignments = append(alignments, api.PromotionFreightAlignment{
			Origin: alignment.Origin, SourceStageName: alignment.SourceStageName,
			SourceStageID: alignment.SourceStageID, TargetStageID: alignment.TargetStageID,
			Status:               api.PromotionFreightAlignmentStatus(alignment.Status),
			SourceCurrentFreight: promotionFreightItemPointerResponse(alignment.SourceCurrentFreight),
			TargetCurrentFreight: promotionFreightItemPointerResponse(alignment.TargetCurrentFreight),
			Reason:               optionalString(alignment.Reason),
		})
	}

	return api.PromotionProjectStage{
		ID: stage.ID, Name: stage.Name, KubernetesUID: stage.KubernetesUID, ResourceVersion: stage.ResourceVersion,
		ObservationStale: value.ObservationStale, ControlFlow: stage.ControlFlow, Conditions: conditions,
		CurrentPromotion: promotionStagePromotionResponse(stage.CurrentPromotion),
		LastPromotion:    promotionStagePromotionResponse(stage.LastPromotion),
		RequestedFreight: requests, FreightAlignments: alignments, Freight: promotionFreightResponse(stage.Freight),
		AutoPromotionEnabled: stage.AutoPromotionEnabled, HealthStatus: stage.HealthStatus,
		Policy: promotionPolicyResponse(value.Policy), PendingPromotionCount: value.PendingPromotionCount,
		Enrichment:      promotionStageEnrichmentResponse(stage.Enrichment()),
		Capabilities:    promotionStageCapabilitiesResponse(value.Capabilities),
		FirstObservedAt: stage.FirstObservedAt, LastObservedAt: stage.LastObservedAt,
		KargoURL: h.kargoIntegrations.KargoStageURL(stage.Integration, stage.Project, stage.Name),
		ArgoCdURL: h.kargoIntegrations.ArgoCDApplicationURL(
			stage.Integration, stage.Environment, valueOrEmptyString(stage.ArgoApplicationNamespace), stage.Application,
		),
	}
}

func promotionFreightItemPointerResponse(value *promotion.FreightItem) *api.PromotionFreightItem {
	if value == nil {
		return nil
	}

	return new(promotionFreightItemResponse(*value))
}

func promotionStageCapabilitiesResponse(value projection.StageCapabilities) api.PromotionStageCapabilities {
	return api.PromotionStageCapabilities{
		Promotion:   promotionStageCapabilityResponse(value.Promotion),
		Review:      promotionStageCapabilityResponse(value.Review),
		BranchDrift: promotionStageCapabilityResponse(value.BranchDrift),
	}
}

func promotionStageCapabilityResponse(value projection.StageCapability) api.PromotionStageCapability {
	return api.PromotionStageCapability{
		Status: api.PromotionStageCapabilityStatus(value.Status),
		Reason: optionalString(value.Reason),
	}
}

func promotionStagePromotionResponse(value *promotion.StagePromotionReference) *api.PromotionStagePromotionReference {
	if value == nil {
		return nil
	}

	return &api.PromotionStagePromotionReference{
		Name: value.Name, Phase: optionalString(value.Phase), FinishedAt: value.FinishedAt,
	}
}
