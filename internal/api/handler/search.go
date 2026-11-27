package handler

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/promotion"
)

type SearchStore interface {
	SearchTargets(context.Context, []string, int32) ([]catalog.TargetSearchResult, error)
	SearchPromotionInventory(context.Context, promotion.IntegrationSet, []string, int32) (promotion.InventorySearchResults, error)
}

func (h *Handler) SearchResources(
	ctx context.Context, request api.SearchResourcesRequestObject,
) (api.SearchResourcesResponseObject, error) {
	const perKind = 5
	query := request.Params.Q
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 || utf8.RuneCountInString(query) > 200 {
		problem := NewProblem(http.StatusBadRequest, "q must contain a search term and be at most 200 characters")
		return api.SearchResources400ApplicationProblemPlusJSONResponse(problem), nil
	}

	targets, err := h.search.SearchTargets(ctx, terms, perKind)
	if err != nil {
		return nil, err
	}
	inventory, err := h.search.SearchPromotionInventory(ctx, h.kargoIntegrations, terms, perKind)
	if err != nil {
		return nil, err
	}
	result := api.SearchResources200JSONResponse{
		Targets:   make([]api.TargetSearchResult, 0, len(targets)),
		Stages:    kargoSearchResults(inventory.Stages),
		Pipelines: kargoSearchResults(inventory.Pipelines),
	}
	for _, target := range targets {
		result.Targets = append(result.Targets, api.TargetSearchResult{
			ID: target.ID, Name: target.Name, Title: target.Title, Repository: target.Repository,
		})
	}
	return result, nil
}

func kargoSearchResults(items []promotion.InventorySearchResult) []api.KargoSearchResult {
	results := make([]api.KargoSearchResult, 0, len(items))
	for _, item := range items {
		results = append(results, api.KargoSearchResult{
			Name: item.Name, StageID: item.StageID, StageName: item.StageName,
			Project: item.Project, Integration: item.Integration, Cluster: item.ClusterName,
		})
	}
	return results
}
