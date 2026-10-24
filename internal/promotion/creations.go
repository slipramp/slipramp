package promotion

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

const maximumKargoPromotionNameLength = 253

type OperatorPromotionRequest struct {
	TargetStageID uuid.UUID
	SourceStageID uuid.UUID
	FreightName   string
	Principal     productidentity.Principal
}

type OperatorPromotionAcceptance struct {
	ID                         uuid.UUID
	Integration                string
	TargetStageID              uuid.UUID
	SourceStageID              uuid.UUID
	FreightName                string
	RequestedByPrincipalID     uuid.UUID
	RequestedBySnapshot        productidentity.ActorSnapshot
	AcceptedAt                 time.Time
	MaximumStageObservationAge time.Duration
	PendingReviewRetention     time.Duration
}

func CreationExternalName(stageName string, promotionID uuid.UUID) string {
	suffix := "-slipramp-" + promotionID.String()
	maximumPrefixLength := maximumKargoPromotionNameLength - len(suffix)
	if len(stageName) > maximumPrefixLength {
		stageName = strings.TrimRight(stageName[:maximumPrefixLength], "-.")
	}

	return stageName + suffix
}

type OperatorPromotionStore interface {
	GetPromotionStage(context.Context, uuid.UUID) (Stage, error)
	CreateOperatorPromotion(context.Context, OperatorPromotionAcceptance) (Promotion, error)
}

type CreationService struct {
	integrations IntegrationSet
	store        OperatorPromotionStore
	newID        func() uuid.UUID
	now          func() time.Time
}

func NewCreationService(integrations IntegrationSet, store OperatorPromotionStore) *CreationService {
	return &CreationService{
		integrations: integrations,
		store:        store,
		newID:        uuid.New,
		now:          time.Now,
	}
}

func (s *CreationService) Create(
	ctx context.Context,
	request OperatorPromotionRequest,
) (Promotion, error) {
	if request.TargetStageID == uuid.Nil || request.SourceStageID == uuid.Nil {
		return Promotion{}, fmt.Errorf("%w: target and source Stage IDs are required", ErrInvalid)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Freight name", request.FreightName); err != nil {
		return Promotion{}, invalid(err)
	}
	if err := request.Principal.Validate(); err != nil {
		return Promotion{}, invalid(err)
	}
	if request.Principal.Role != productidentity.RoleOperator && request.Principal.Role != productidentity.RoleAdmin {
		return Promotion{}, fmt.Errorf("%w: requesting principal must be an operator", ErrInvalid)
	}
	actor, err := request.Principal.ActorSnapshot()
	if err != nil {
		return Promotion{}, invalid(err)
	}

	target, err := s.store.GetPromotionStage(ctx, request.TargetStageID)
	if err != nil {
		return Promotion{}, err
	}
	integration, ok := s.integrations.Lookup(target.Integration)
	if !ok {
		return Promotion{}, ErrIntegrationNotFound
	}
	acceptedAt := s.now().UTC().Truncate(time.Microsecond)
	acceptance := OperatorPromotionAcceptance{
		ID:                         s.newID(),
		Integration:                target.Integration,
		TargetStageID:              request.TargetStageID,
		SourceStageID:              request.SourceStageID,
		FreightName:                request.FreightName,
		RequestedByPrincipalID:     request.Principal.ID,
		RequestedBySnapshot:        actor,
		AcceptedAt:                 acceptedAt,
		MaximumStageObservationAge: integration.MaximumStageObservationAge,
		PendingReviewRetention:     integration.PendingReviewRetention,
	}
	if err := acceptance.Validate(); err != nil {
		return Promotion{}, err
	}

	return s.store.CreateOperatorPromotion(ctx, acceptance)
}

func (a OperatorPromotionAcceptance) Validate() error {
	if a.ID == uuid.Nil || a.TargetStageID == uuid.Nil || a.SourceStageID == uuid.Nil ||
		a.RequestedByPrincipalID == uuid.Nil {
		return fmt.Errorf("%w: Promotion, Stage, and principal IDs are required", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", a.Integration); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Freight name", a.FreightName); err != nil {
		return invalid(err)
	}
	if a.AcceptedAt.IsZero() || a.MaximumStageObservationAge <= 0 || a.PendingReviewRetention <= 0 {
		return fmt.Errorf("%w: creation time and retention periods are required", ErrInvalid)
	}
	if a.RequestedBySnapshot.PrincipalID != a.RequestedByPrincipalID {
		return fmt.Errorf("%w: requesting actor snapshot does not match its principal", ErrInvalid)
	}
	if err := a.RequestedBySnapshot.Validate(); err != nil {
		return invalid(err)
	}

	return nil
}

func SelectPromotionFreight(target Stage, source Stage, name string) (*FreightItem, error) {
	if target.Integration != source.Integration || target.ClusterName != source.ClusterName ||
		target.Project != source.Project {
		return nil, ErrStageScope
	}

	var selected *FreightItem
	for index := range source.Freight.Items {
		if source.Freight.Items[index].Name != name {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("%w: Freight name %q is ambiguous", ErrInvalid, name)
		}
		selected = &source.Freight.Items[index]
	}
	if selected == nil {
		return nil, ErrFreightNotFound
	}

	if target.ID != source.ID {
		accepted := false
		for _, request := range target.RequestedFreight {
			if request.Origin == selected.Origin && slices.Contains(request.SourceStages, source.Name) {
				accepted = true
				break
			}
		}
		if !accepted {
			return nil, ErrFreightSource
		}
	}

	cloned := cloneFreightItem(*selected)
	return &cloned, nil
}
