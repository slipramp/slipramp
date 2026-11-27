package handler

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/promotion"
)

type targetOnlySearchStore struct {
	terms []string
}

func (s *targetOnlySearchStore) SearchTargets(
	_ context.Context, terms []string, _ int32,
) ([]catalog.TargetSearchResult, error) {
	s.terms = terms
	return []catalog.TargetSearchResult{{Name: "prod", Title: "Production", Repository: "payments"}}, nil
}

func (*targetOnlySearchStore) SearchPromotionInventory(
	context.Context, promotion.IntegrationSet, []string, int32,
) (promotion.InventorySearchResults, error) {
	return promotion.InventorySearchResults{}, nil
}

func TestSearchResources(t *testing.T) {
	store := new(targetOnlySearchStore)
	h := newHandler(Dependencies{Search: store})
	search := func(query string) (api.SearchResourcesResponseObject, error) {
		return h.SearchResources(t.Context(), api.SearchResourcesRequestObject{
			Params: api.SearchResourcesParams{Q: query},
		})
	}

	resp, err := search("  PAYMENTS   pRoD  ")
	result, ok := resp.(api.SearchResources200JSONResponse)
	if err != nil || !ok || !slices.Equal(store.terms, []string{"payments", "prod"}) {
		t.Fatalf("search response = %#v, terms = %v, err = %v", resp, store.terms, err)
	}
	if len(result.Targets) != 1 || result.Stages == nil || result.Pipelines == nil ||
		len(result.Stages) != 0 || len(result.Pipelines) != 0 {
		t.Fatalf("search results = %+v", result)
	}

	for _, query := range []string{"   ", strings.Repeat("é", 201)} {
		resp, err := search(query)
		result, ok := resp.(api.SearchResources400ApplicationProblemPlusJSONResponse)
		if err != nil || !ok || result.Status != http.StatusBadRequest {
			t.Fatalf("invalid search response = %#v, err = %v", resp, err)
		}
	}
}
