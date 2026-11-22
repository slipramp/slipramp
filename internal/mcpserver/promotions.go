package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/promotion/projection"
)

type StageInventory interface {
	ListStages(context.Context, projection.StageListFilter) (projection.StageList, error)
}

type PromotionInventory interface {
	ListPromotions(context.Context, promotion.PromotionListFilter) (promotion.PromotionReviewList, error)
}

type promotionInventoryInput struct {
	Integration string `json:"integration,omitempty" jsonschema:"Optional Kargo integration name."`
	Cluster     string `json:"cluster,omitempty" jsonschema:"Optional Cluster name."`
	Project     string `json:"project,omitempty" jsonschema:"Optional Kargo Project (namespace)."`
	Limit       int32  `json:"limit,omitempty" jsonschema:"Maximum items from 1 to 200. Defaults to 50."`
	Offset      int32  `json:"offset,omitempty" jsonschema:"Zero-based pagination offset."`
}

func (input promotionInventoryInput) limit() int {
	if input.Limit == 0 {
		return promotion.DefaultListLimit
	}
	return int(input.Limit)
}

type listPromotionsInput struct {
	promotionInventoryInput
	StageID string `json:"stageId,omitempty" jsonschema:"Optional destination Stage UUID from list_promotion_stages."`
}

type promotionInventoryPage[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type stageOutput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Integration string `json:"integration"`
	Cluster     string `json:"cluster"`
	Project     string `json:"project"`
}

type promotionOutput struct {
	ID                  string                   `json:"id"`
	ExternalPromotionID string                   `json:"externalPromotionId"`
	Stage               stageOutput              `json:"stage"`
	Lifecycle           promotion.LifecycleState `json:"lifecycle"`
	CreatedAt           time.Time                `json:"createdAt"`
}

func (s *toolHandler) listPromotionStages(ctx context.Context, _ *mcp.CallToolRequest, input promotionInventoryInput) (
	*mcp.CallToolResult, promotionInventoryPage[stageOutput], error,
) {
	var page promotionInventoryPage[stageOutput]
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, page, err
	}
	list, err := s.stages.ListStages(ctx, projection.StageListFilter{
		Integration: input.Integration, Cluster: input.Cluster, Project: input.Project,
		Limit: input.limit(), Offset: int(input.Offset),
	})
	if errors.Is(err, promotion.ErrInvalid) {
		return nil, page, err
	}
	if err != nil {
		return nil, page, s.internalError("list Promotion Stages", err)
	}
	page = promotionInventoryPage[stageOutput]{
		Items: make([]stageOutput, 0, len(list.Items)), Limit: list.Limit, Offset: list.Offset, Total: list.Total,
	}
	for _, item := range list.Items {
		stage := item.Stage.Stage
		page.Items = append(page.Items, stageOutput{
			ID: stage.ID.String(), Name: stage.Name, Integration: item.Integration,
			Cluster: item.ClusterName, Project: item.Project,
		})
	}
	return nil, page, nil
}

func (s *toolHandler) listPromotions(ctx context.Context, _ *mcp.CallToolRequest, input listPromotionsInput) (
	*mcp.CallToolResult, promotionInventoryPage[promotionOutput], error,
) {
	var page promotionInventoryPage[promotionOutput]
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, page, err
	}
	stageID, err := optionalUUID(input.StageID, "stageId")
	if err != nil {
		return nil, page, err
	}
	list, err := s.promotions.ListPromotions(ctx, promotion.PromotionListFilter{
		Integration: input.Integration, Cluster: input.Cluster, Project: input.Project, StageID: stageID,
		Limit: input.limit(), Offset: int(input.Offset),
	})
	if errors.Is(err, promotion.ErrInvalid) {
		return nil, page, err
	}
	if err != nil {
		return nil, page, s.internalError("list Promotions", err)
	}
	page = promotionInventoryPage[promotionOutput]{
		Items: make([]promotionOutput, 0, len(list.Items)), Limit: list.Limit, Offset: list.Offset, Total: list.Total,
	}
	for _, item := range list.Items {
		record := item.Promotion
		stage := record.StageSnapshot
		page.Items = append(page.Items, promotionOutput{
			ID: record.ID.String(), ExternalPromotionID: record.ExternalPromotionID,
			Stage: stageOutput{ID: record.StageID.String(), Name: stage.Name, Integration: record.Integration,
				Cluster: stage.ClusterName, Project: stage.Project},
			Lifecycle: record.Lifecycle.State, CreatedAt: record.CreatedAt,
		})
	}
	return nil, page, nil
}
