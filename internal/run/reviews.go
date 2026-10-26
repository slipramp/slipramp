package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

const MaximumReviewCommentLength = 4096

type ReviewStore interface {
	DecideRunReview(context.Context, ReviewTransition) (Run, error)
}

type ReviewTransition struct {
	RunID     uuid.UUID
	Decision  ReviewDecision
	Comment   *string
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
	comment, err := normalizeReviewComment(request.Comment)
	if err != nil {
		return Run{}, err
	}

	record, err := s.reviews.DecideRunReview(ctx, ReviewTransition{
		RunID:     request.RunID,
		Decision:  request.Decision,
		Comment:   comment,
		DecidedBy: actor,
		DecidedAt: s.now().UTC(),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrRunNotFound), errors.Is(err, ErrReviewConflict):
			return Run{}, err
		default:
			return Run{}, fmt.Errorf("decide Run review: %w", err)
		}
	}

	return record, nil
}

// ValidateReviewTransition owns the state machine independently from its
// PostgreSQL transaction boundary. An identical queued approval or terminal
// rejection from the same Principal with the same comment is the only
// idempotent retry.
func ValidateReviewTransition(current ReviewTransitionState, transition ReviewTransition) error {
	if !transition.Decision.valid() || transition.DecidedAt.IsZero() {
		return fmt.Errorf("%w: invalid review decision", ErrInvalidRequest)
	}
	if err := transition.DecidedBy.Validate(); err != nil {
		return fmt.Errorf("%w: invalid review actor: %w", ErrInvalidRequest, err)
	}
	if err := validateReviewComment(transition.Comment); err != nil {
		return err
	}

	if current.Review.State == ReviewStateApproved || current.Review.State == ReviewStateRejected {
		if current.Review.DecidedBy == nil || current.Review.DecidedAt == nil ||
			current.Review.State != ReviewState(transition.Decision) ||
			current.Review.DecidedBy.PrincipalID != transition.DecidedBy.PrincipalID ||
			!equalOptionalString(current.Review.Comment, transition.Comment) {
			return ErrReviewConflict
		}

		switch current.Review.State {
		case ReviewStateApproved:
			if current.Phase == PhaseQueued {
				return nil
			}
		case ReviewStateRejected:
			if current.Phase == PhaseFinished {
				return nil
			}
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

func normalizeReviewComment(comment *string) (*string, error) {
	if comment == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*comment)
	if trimmed == "" {
		return nil, nil
	}
	if !utf8.ValidString(trimmed) || utf8.RuneCountInString(trimmed) > MaximumReviewCommentLength {
		return nil, fmt.Errorf(
			"%w: review comment must be at most %d characters",
			ErrInvalidRequest,
			MaximumReviewCommentLength,
		)
	}

	return &trimmed, nil
}

func validateReviewComment(comment *string) error {
	if comment == nil {
		return nil
	}
	if *comment == "" || *comment != strings.TrimSpace(*comment) || !utf8.ValidString(*comment) ||
		utf8.RuneCountInString(*comment) > MaximumReviewCommentLength {
		return fmt.Errorf("%w: invalid review comment", ErrInvalidRequest)
	}

	return nil
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return *left == *right
}
