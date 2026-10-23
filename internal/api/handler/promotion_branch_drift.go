package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

type PromotionBranchDriftService interface {
	List(context.Context, promotion.BranchDriftListFilter) (promotion.BranchDriftList, error)
}

func (h *Handler) ListPromotionBranchDrift(
	ctx context.Context,
	request api.ListPromotionBranchDriftRequestObject,
) (api.ListPromotionBranchDriftResponseObject, error) {
	filter := promotion.BranchDriftListFilter{
		Integration: optionalCatalogName(request.Params.Integration),
		Repository:  optionalCatalogName(request.Params.Repository),
		Limit:       promotion.DefaultListLimit,
	}
	if request.Params.Limit != nil {
		filter.Limit = int(*request.Params.Limit)
	}
	if request.Params.Offset != nil {
		filter.Offset = int(*request.Params.Offset)
	}

	result, err := h.promotionDrift.List(ctx, filter)
	if errors.Is(err, promotion.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListPromotionBranchDrift400ApplicationProblemPlusJSONResponse{
			BadRequestApplicationProblemPlusJSONResponse: api.BadRequestApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	items := make([]api.PromotionBranchDriftRepository, 0, len(result.Items))
	for _, item := range result.Items {
		stages := make([]api.PromotionBranchDriftStage, 0, len(item.Stages))
		for _, stage := range item.Stages {
			response := api.PromotionBranchDriftStage{
				ID:             stage.Stage.ID,
				Integration:    api.CatalogName(stage.Stage.Integration),
				Cluster:        api.CatalogName(stage.Stage.ClusterName),
				Project:        api.CatalogName(stage.Stage.Project),
				Name:           stage.Stage.Name,
				Application:    stage.Stage.Application,
				Environment:    stage.Stage.Environment,
				TargetBranch:   stage.Stage.TargetBranch,
				SourcePaths:    stage.Stage.SourcePaths,
				Status:         api.PromotionBranchDriftStatus(stage.Status),
				LastObservedAt: stage.Stage.LastObservedAt,
			}
			response.AcknowledgedSourceRevision = optionalGitRevision(stage.AcknowledgedSourceRevision)
			response.AcknowledgedRenderBaseRevision = optionalGitRevision(
				stage.AcknowledgedRenderBaseRevision,
			)
			response.AcknowledgedRenderRevision = optionalGitRevision(stage.AcknowledgedRenderRevision)
			response.SourceBranchRevision = optionalGitRevision(stage.SourceBranchRevision)
			response.TargetBranchRevision = optionalGitRevision(stage.TargetBranchRevision)
			if stage.AcknowledgedSourceRevision != "" {
				response.NoOpRender = new(stage.NoOpRender)
			}
			if stage.Status != promotion.BranchDriftStatusUnknown {
				response.SourceDrift = new(stage.SourceDrift)
				response.TargetDrift = new(stage.TargetDrift)
			}
			if stage.Message != "" {
				response.Message = new(stage.Message)
			}
			stages = append(stages, response)
		}

		response := api.PromotionBranchDriftRepository{
			Repository: api.CatalogName(item.Repository.Name),
			ScannedAt:  item.ScannedAt,
			Stages:     stages,
		}
		if item.Repository.Branch != "" {
			response.SourceBranch = new(item.Repository.Branch)
		}
		if item.Error != "" {
			response.Error = new(item.Error)
		}
		items = append(items, response)
	}

	return api.ListPromotionBranchDrift200JSONResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
}

func optionalGitRevision(value string) *api.GitRevision {
	if value == "" {
		return nil
	}

	revision := api.GitRevision(value)

	return &revision
}
