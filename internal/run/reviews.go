package run

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

type ReviewStore interface {
	DecideRunReview(context.Context, ReviewTransition) (Run, error)
}

type ReviewTransition struct {
	RunID     uuid.UUID
	Decision  ReviewDecision
	DecidedBy productidentity.ActorSnapshot
	DecidedAt time.Time
}

type ReviewTransitionState struct {
	Phase  Phase
	Review Review
}

func (s *Service) DecideRunReview(ctx context.Context, request ReviewDecisionRequest) (Run, error) {
	if request.RunID == uuid.Nil {
		return Run{}, fmt.Errorf("%w: Run ID is required", ErrInvalidRequest)
	}
	if !request.Decision.valid() {
		return Run{}, fmt.Errorf("%w: review decision is invalid", ErrInvalidRequest)
	}
	if request.Actor == nil {
		return Run{}, fmt.Errorf("%w: review actor is required", ErrInvalidRequest)
	}
	actor, err := request.Actor.ActorSnapshot()
	if err != nil {
		return Run{}, fmt.Errorf("%w: review actor is invalid: %w", ErrInvalidRequest, err)
	}

	record, err := s.reviews.DecideRunReview(ctx, ReviewTransition{
		RunID:     request.RunID,
		Decision:  request.Decision,
		DecidedBy: actor,
		DecidedAt: s.now().UTC(),
	})
	if err != nil {
		return Run{}, err
	}

	return record, nil
}

// ValidateReviewTransition owns the state machine independently from its
// PostgreSQL transaction boundary. An identical queued approval or terminal
// rejection from the same Principal is the only idempotent retry.
func ValidateReviewTransition(current ReviewTransitionState, transition ReviewTransition) error {
	if !transition.Decision.valid() || transition.DecidedAt.IsZero() {
		return fmt.Errorf("%w: invalid review decision", ErrInvalidRequest)
	}
	if err := transition.DecidedBy.Validate(); err != nil {
		return fmt.Errorf("%w: invalid review actor: %w", ErrInvalidRequest, err)
	}

	if current.Review.State == ReviewStateApproved || current.Review.State == ReviewStateRejected {
		matchesExisting := current.Review.DecidedBy != nil &&
			current.Review.State == ReviewState(transition.Decision) &&
			current.Review.DecidedBy.PrincipalID == transition.DecidedBy.PrincipalID
		if matchesExisting &&
			(current.Review.State == ReviewStateRejected || current.Phase == PhaseQueued) {
			return nil
		}

		return ErrReviewConflict
	}

	if current.Phase != PhaseQueued || current.Review.State != ReviewStatePending ||
		current.Review.DecidedBy != nil || current.Review.DecidedAt != nil {
		return ErrReviewConflict
	}

	return nil
}

func (d ReviewDecision) valid() bool {
	return d == ReviewDecisionApproved || d == ReviewDecisionRejected
}
