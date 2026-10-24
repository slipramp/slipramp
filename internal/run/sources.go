package run

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func validateSource(source Source) error {
	switch source.Type {
	case SourceTypeManual, SourceTypeAPI:
		if source.Git != nil || source.Promotion != nil {
			return errors.New("manual and API sources must not contain automated-source details")
		}
	case SourceTypeGit:
		if source.Git == nil || source.Promotion != nil || source.Git.DeliveryID == uuid.Nil ||
			strings.TrimSpace(source.Git.TriggerName) == "" {
			return errors.New("git source requires a trigger name and delivery ID")
		}
	case SourceTypePromotion:
		if source.Promotion == nil || source.Git != nil || source.Promotion.PromotionID == uuid.Nil {
			return errors.New("promotion source requires a Promotion ID")
		}
	default:
		return errors.New("unknown source type")
	}

	return nil
}

func validateSourceRequest(request CreateRequest) error {
	switch request.Source.Type {
	case SourceTypeManual, SourceTypeAPI:
		if request.CatalogRevisionID != nil {
			return errors.New("manual and API Runs cannot pin a catalog revision")
		}

		return nil
	case SourceTypeGit:
		if request.Actor != nil {
			return errors.New("git-triggered Runs must not have an actor")
		}
		if request.CatalogRevisionID == nil {
			return errors.New("git-triggered Runs must pin a catalog revision")
		}

		deliveryKey := request.Source.Git.DeliveryID.String()
		if request.IdempotencyKey == nil || *request.IdempotencyKey != deliveryKey {
			return errors.New("git-triggered Runs must use the delivery ID as their idempotency key")
		}
	case SourceTypePromotion:
		if request.Actor == nil {
			return errors.New("promotion preparation Runs require the requesting actor")
		}
		if request.CatalogRevisionID == nil {
			return errors.New("promotion preparation Runs must pin a catalog revision")
		}

		promotionKey := request.Source.Promotion.PromotionID.String()
		if request.IdempotencyKey == nil || *request.IdempotencyKey != promotionKey {
			return errors.New("promotion preparation Runs must use the Promotion ID as their idempotency key")
		}
	}

	return nil
}

func actorSnapshot(principal *productidentity.Principal) (*productidentity.ActorSnapshot, error) {
	if principal == nil {
		return nil, nil
	}

	snapshot, err := principal.ActorSnapshot()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}

	return &snapshot, nil
}

func actorPrincipalID(principal *productidentity.Principal) *uuid.UUID {
	if principal == nil {
		return nil
	}

	principalID := principal.ID

	return &principalID
}
