package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/promotion/projection"
)

type recordingPromotionInventory struct {
	stageFilter     projection.StageListFilter
	stages          projection.StageList
	promotionFilter promotion.PromotionListFilter
	promotions      promotion.PromotionReviewList
}

func (r *recordingPromotionInventory) ListStages(
	_ context.Context, filter projection.StageListFilter,
) (projection.StageList, error) {
	r.stageFilter = filter
	return r.stages, nil
}

func (r *recordingPromotionInventory) ListPromotions(
	_ context.Context, filter promotion.PromotionListFilter,
) (promotion.PromotionReviewList, error) {
	r.promotionFilter = filter
	return r.promotions, nil
}

func TestDiscoverStageAndPromotionForGateTools(t *testing.T) {
	t.Parallel()
	stageID, promotionID := uuid.New(), uuid.New()
	inventory := &recordingPromotionInventory{
		stages: projection.StageList{Items: []projection.StageDetail{{
			Integration: "delivery", ClusterName: "homelab", Project: "apps",
			Stage: projection.ProjectStage{Stage: promotion.Stage{ID: stageID, Name: "prod"}},
		}}, Total: 1, Limit: promotion.DefaultListLimit},
		promotions: promotion.PromotionReviewList{Items: []promotion.PromotionReview{{
			Promotion: promotion.Promotion{
				ID: promotionID, Integration: "delivery", StageID: stageID, CreatedAt: time.Now().UTC(),
				StageSnapshot:       promotion.PromotionStageSnapshot{Name: "prod", Project: "apps", ClusterName: "homelab"},
				ExternalPromotionID: "rollout", Lifecycle: promotion.PromotionLifecycle{State: promotion.LifecycleApproved},
			},
		}}, Total: 3, Offset: 2, Limit: 1},
	}
	auth := testAuthentication(productidentity.ScopeRead)
	auth.Principal.Role = productidentity.RoleViewer
	deps := testDependencies(auth)
	deps.Stages, deps.Promotions = inventory, inventory
	handler, err := NewHTTP(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	client := connectTestClient(t, handler, testAPIKey)
	var stages promotionInventoryPage[stageOutput]
	args := map[string]any{"project": "apps", "integration": "delivery", "cluster": "homelab"}
	decodeStructuredContent(t, callTool(t, client, "list_promotion_stages", args), &stages)
	wantFilter := projection.StageListFilter{
		Project: "apps", Integration: "delivery", Cluster: "homelab", Limit: promotion.DefaultListLimit,
	}
	if inventory.stageFilter != wantFilter ||
		len(stages.Items) != 1 || stages.Items[0].ID != stageID.String() || stages.Items[0].Name != "prod" {
		t.Fatalf("Stage filter = %#v, page = %#v", inventory.stageFilter, stages)
	}
	var records promotionInventoryPage[promotionOutput]
	args["stageId"], args["limit"], args["offset"] = stages.Items[0].ID, 1, 2
	decodeStructuredContent(t, callTool(t, client, "list_promotions", args), &records)
	filter := inventory.promotionFilter
	if filter.StageID == nil || *filter.StageID != stageID || filter.Limit != 1 || filter.Offset != 2 ||
		len(records.Items) != 1 || records.Items[0].ID != promotionID.String() || records.Items[0].Stage != stages.Items[0] ||
		records.Total != 3 || records.Offset != 2 || records.Items[0].Lifecycle != promotion.LifecycleApproved {
		t.Fatalf("Promotion filter = %#v, page = %#v", filter, records)
	}
	rejectedGateTool(t, client, "list_promotions", map[string]any{"stageId": "bad"}, "UUID")
}
