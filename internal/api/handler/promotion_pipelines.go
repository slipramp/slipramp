package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

func (h *Handler) ListPromotionPipelines(
	ctx context.Context,
	request api.ListPromotionPipelinesRequestObject,
) (api.ListPromotionPipelinesResponseObject, error) {
	filter := promotion.FreightPipelineListFilter{
		Integration: optionalCatalogName(request.Params.Integration),
		Cluster:     optionalCatalogName(request.Params.Cluster),
		Project:     optionalCatalogName(request.Params.Project),
		Repository:  optionalCatalogName(request.Params.Repository),
		Limit:       promotion.DefaultListLimit,
	}
	if request.Params.Application != nil {
		filter.Application = *request.Params.Application
	}
	if request.Params.Status != nil {
		filter.Status = promotion.FreightComparisonStatus(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		filter.Limit = int(*request.Params.Limit)
	}
	if request.Params.Offset != nil {
		filter.Offset = int(*request.Params.Offset)
	}

	result, err := h.promotionPipelines.List(ctx, filter)
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
		stages := make([]api.PromotionPipelineStage, 0, len(pipeline.Stages))
		for _, pipelineStage := range pipeline.Stages {
			freight := promotionFreightResponse(pipelineStage.Stage.Freight)
			promotionSources := make([]api.PromotionFreightSource, 0, len(pipelineStage.PromotionSources))
			for _, source := range pipelineStage.PromotionSources {
				items := make([]api.PromotionFreightItem, 0, len(source.Freight))
				for _, item := range source.Freight {
					items = append(items, promotionFreightItemResponse(item))
				}
				promotionSources = append(promotionSources, api.PromotionFreightSource{
					StageID:     source.StageID,
					StageName:   source.StageName,
					Environment: source.Environment,
					Freight:     api.PromotionFreight{Items: items},
				})
			}
			stages = append(stages, api.PromotionPipelineStage{
				ID:                    pipelineStage.Stage.ID,
				Name:                  pipelineStage.Stage.Name,
				Environment:           pipelineStage.Stage.Environment,
				Freight:               freight,
				PromotionSources:      promotionSources,
				PendingPromotionCount: pipelineStage.PendingPromotionCount,
				LastObservedAt:        pipelineStage.Stage.LastObservedAt,
				KargoURL: h.kargoIntegrations.KargoStageURL(
					pipeline.Integration,
					pipeline.Project,
					pipelineStage.Stage.Name,
				),
			})
		}
		items = append(items, api.PromotionPipeline{
			Integration:      api.CatalogName(pipeline.Integration),
			Cluster:          api.CatalogName(pipeline.ClusterName),
			Project:          api.CatalogName(pipeline.Project),
			Repository:       api.CatalogName(pipeline.Repository),
			Application:      pipeline.Application,
			EnvironmentOrder: pipeline.EnvironmentOrder,
			Stages:           stages,
		})
	}

	return api.ListPromotionPipelines200JSONResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
}

func promotionFreightItemResponse(item promotion.FreightItem) api.PromotionFreightItem {
	artifacts := make([]api.PromotionFreightArtifact, 0, len(item.Artifacts))
	for _, artifact := range item.Artifacts {
		artifacts = append(artifacts, api.PromotionFreightArtifact{
			Kind:       api.PromotionFreightArtifactKind(artifact.Kind),
			Repository: artifact.Repository,
			Version:    artifact.Version,
		})
	}

	return api.PromotionFreightItem{
		Origin:    item.Origin,
		Name:      item.Name,
		Artifacts: artifacts,
	}
}
