package handler

import (
	"context"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gittrigger"
)

type GitTriggerDeliveryService interface {
	ListFailedGitTriggerDeliveries(context.Context, int, int) (gittrigger.FailedDeliveryPage, error)
}

func (h *Handler) ListFailedGitTriggerDeliveries(
	ctx context.Context, request api.ListFailedGitTriggerDeliveriesRequestObject,
) (api.ListFailedGitTriggerDeliveriesResponseObject, error) {
	limit, offset, err := listWindow(request.Params.Limit, request.Params.Offset)
	if err != nil {
		return api.ListFailedGitTriggerDeliveries400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	}
	page, err := h.gitTriggerDeliveries.ListFailedGitTriggerDeliveries(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	items := make([]api.FailedGitTriggerDelivery, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, api.FailedGitTriggerDelivery{
			ID:                item.ID,
			RepositoryID:      item.RepositoryID,
			RepositoryName:    item.RepositoryName,
			CatalogRevisionID: item.CatalogRevisionID,
			CommitSha:         item.CommitSHA,
			Target:            item.TargetName,
			Action:            item.ActionName,
			Trigger:           item.TriggerName,
			FailureCount:      item.FailureCount,
			Diagnostic:        item.Diagnostic,
			FailedAt:          item.FailedAt,
		})
	}
	return api.ListFailedGitTriggerDeliveries200JSONResponse{
		Items:  items,
		Total:  page.Total,
		Limit:  limit,
		Offset: offset,
	}, nil
}
