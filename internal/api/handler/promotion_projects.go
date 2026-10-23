package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

type PromotionProjectService interface {
	List(context.Context, promotion.ProjectListFilter) (promotion.ProjectList, error)
}

func (h *Handler) ListPromotionProjects(
	ctx context.Context,
	request api.ListPromotionProjectsRequestObject,
) (api.ListPromotionProjectsResponseObject, error) {
	filter := promotion.ProjectListFilter{
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
			Integration:      api.CatalogName(project.Integration),
			Cluster:          api.CatalogName(project.ClusterName),
			Project:          api.CatalogName(project.Project),
			StageCount:       project.StageCount,
			ApplicationCount: project.ApplicationCount,
			LastObservedAt:   project.LastObservedAt,
			KargoURL: h.kargoIntegrations.KargoProjectURL(
				project.Integration,
				project.Project,
			),
		})
	}

	return api.ListPromotionProjects200JSONResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
}
