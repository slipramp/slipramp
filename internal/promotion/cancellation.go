package promotion

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/cancellation"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type Cancellation struct {
	RequestedAt *time.Time
	RequestedBy *productidentity.ActorSnapshot
	ConfirmedAt *time.Time
}

func (p Promotion) CancellationState() cancellation.State {
	switch {
	case p.Cancellation.RequestedAt == nil:
		return cancellation.NotRequested
	case p.Cancellation.ConfirmedAt != nil:
		return cancellation.Completed
	case p.KargoExecution.Outcome != nil || p.CreationDispatch.FailedAt != nil:
		return cancellation.Overtaken
	default:
		return cancellation.Requested
	}
}

func (p Promotion) CanRequestCancellation(at time.Time) bool {
	if p.Cancellation.RequestedAt != nil || p.KargoExecution.Outcome != nil || p.CreationDispatch.FailedAt != nil {
		return false
	}
	switch p.Lifecycle.State {
	case LifecycleCreating, LifecyclePreparing, LifecycleApproved:
		return true
	case LifecyclePending:
		return p.Lifecycle.ExpiresAt == nil || p.Lifecycle.ExpiresAt.After(at)
	default:
		return false
	}
}

var ErrNoCancellation = errors.New("no Promotion cancellation is due")

type CancellationStore interface {
	CancelPromotion(context.Context, uuid.UUID, time.Time, productidentity.ActorSnapshot) (Promotion, string, error)
	ClaimPromotionCancellation(context.Context, string, time.Time, time.Time) (Promotion, error)
}

type CancellationService struct {
	store  CancellationStore
	notify func(string)
}

func NewCancellationService(store CancellationStore, notify func(string)) *CancellationService {
	return &CancellationService{store: store, notify: notify}
}

func (s *CancellationService) Cancel(ctx context.Context, id uuid.UUID, principal productidentity.Principal) error {
	actor, err := principal.ActorSnapshot()
	if err != nil {
		return err
	}
	record, prepCluster, err := s.store.CancelPromotion(ctx, id, time.Now().UTC().Truncate(time.Microsecond), actor)
	if err != nil {
		return err
	}
	s.notify(record.StageSnapshot.ClusterName)
	if prepCluster != "" && prepCluster != record.StageSnapshot.ClusterName {
		s.notify(prepCluster)
	}
	return nil
}

func (s *CancellationService) Next(ctx context.Context, cluster string) (ReconciliationCommand, error) {
	now := time.Now().UTC()
	record, err := s.store.ClaimPromotionCancellation(ctx, cluster, now, now.Add(DefaultReconciliationInterval))
	if err != nil {
		return ReconciliationCommand{}, err
	}
	command := reconciliationCommand(record)
	if record.Source == SourceSlipramp && command.KargoUID == nil {
		recovery, err := creationCommand(record)
		if err != nil {
			return ReconciliationCommand{}, err
		}
		command.Recovery = &recovery
	}
	return command, command.Validate()
}
