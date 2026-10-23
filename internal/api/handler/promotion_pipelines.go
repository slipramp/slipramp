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
			freight, decodeErr := promotionFreightResponse(pipelineStage.Stage.Freight)
			if decodeErr != nil {
				return nil, decodeErr
			}
			stages = append(stages, api.PromotionPipelineStage{
				ID:                    pipelineStage.Stage.ID,
				Name:                  pipelineStage.Stage.Name,
				Environment:           pipelineStage.Stage.Environment,
				Freight:               freight,
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
