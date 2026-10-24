package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/notification"
)

type NotificationService interface {
	ListDeliveries(context.Context, notification.DeliveryHistoryFilter) (notification.DeliveryHistoryPage, error)
}

var errNotificationServiceUnavailable = errors.New("notification service is not configured")

func (h *Handler) ListNotificationDeliveries(
	ctx context.Context,
	request api.ListNotificationDeliveriesRequestObject,
) (api.ListNotificationDeliveriesResponseObject, error) {
	limit, offset, err := listWindow(request.Params.Limit, request.Params.Offset)
	if err != nil {
		return api.ListNotificationDeliveries400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	}
	if h.notifications == nil {
		return nil, errNotificationServiceUnavailable
	}

	page, err := h.notifications.ListDeliveries(ctx, notification.DeliveryHistoryFilter{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]api.NotificationDelivery, 0, len(page.Items))
	for _, item := range page.Items {
		lastError := (*string)(nil)
		if item.LastError != "" {
			lastError = new(item.LastError)
		}
		items = append(items, api.NotificationDelivery{
			ID:               item.ID,
			EventID:          item.EventID,
			EventType:        api.NotificationDeliveryEventType(item.EventType),
			SubjectType:      api.NotificationDeliverySubjectType(item.SubjectType),
			SubjectID:        item.SubjectID,
			OccurredAt:       item.OccurredAt,
			DestinationName:  item.DestinationName,
			DestinationType:  api.NotificationDeliveryDestinationType(item.DestinationType),
			State:            api.NotificationDeliveryState(item.State),
			AttemptCount:     item.AttemptCount,
			NextAttemptAt:    item.NextAttemptAt,
			LastError:        lastError,
			FirstAttemptedAt: item.FirstAttemptedAt,
			LastAttemptedAt:  item.LastAttemptedAt,
			DeliveredAt:      item.DeliveredAt,
			FailedAt:         item.FailedAt,
			CreatedAt:        item.CreatedAt,
		})
	}

	return api.ListNotificationDeliveries200JSONResponse{
		Items:  items,
		Limit:  limit,
		Offset: offset,
		Total:  page.Total,
	}, nil
}
