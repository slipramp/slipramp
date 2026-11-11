package inbox

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestPromotionItemSurfacesOperationalFailures(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	diagnostic := "Kargo API rejected the Promotion"
	delivery := inboxPromotion(uuid.New(), promotion.LifecycleError, at)
	delivery.CreationDispatch.FailedAt = &at
	delivery.CreationDispatch.Diagnostic = &diagnostic

	deliveryItem, err := promotionItem(delivery)
	if err != nil {
		t.Fatalf("delivery promotionItem() error = %v", err)
	}
	if deliveryItem.Kind != KindPromotionError || !strings.Contains(deliveryItem.Title, "delivered") ||
		deliveryItem.Description != diagnostic || !deliveryItem.OccurredAt.Equal(at) {
		t.Errorf("delivery item = %#v", deliveryItem)
	}

	outcome := inboxPromotion(uuid.New(), promotion.LifecycleApproved, at)
	outcome.KargoOutcome = &promotion.KargoOutcome{
		Phase: promotion.KargoPhaseFailed, ObservedAt: at, Diagnostic: &diagnostic,
	}
	outcomeItem, err := promotionItem(outcome)
	if err != nil {
		t.Fatalf("outcome promotionItem() error = %v", err)
	}
	if outcomeItem.Kind != KindPromotionError || !strings.Contains(outcomeItem.Title, "failed") ||
		outcomeItem.Description != diagnostic || !outcomeItem.OccurredAt.Equal(at) {
		t.Errorf("outcome item = %#v", outcomeItem)
	}
}
