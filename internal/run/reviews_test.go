package run

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestValidateReviewTransition(t *testing.T) {
	t.Parallel()

	actor := productidentity.ActorSnapshot{
		PrincipalID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
	}
	decidedAt := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	approved := ReviewTransition{Decision: ReviewDecisionApproved, DecidedBy: actor, DecidedAt: decidedAt}

	tests := []struct {
		name       string
		state      ReviewTransitionState
		transition ReviewTransition
		want       error
	}{
		{
			name:       "pending approval",
			state:      ReviewTransitionState{Phase: PhaseQueued, Review: Review{State: ReviewStatePending}},
			transition: approved,
		},
		{
			name: "identical repeated decision",
			state: ReviewTransitionState{Phase: PhaseQueued, Review: Review{
				State: ReviewStateApproved, DecidedBy: &actor, DecidedAt: &decidedAt,
			}},
			transition: approved,
		},
		{
			name: "different repeated decision",
			state: ReviewTransitionState{Phase: PhaseQueued, Review: Review{
				State: ReviewStateApproved, DecidedBy: &actor, DecidedAt: &decidedAt,
			}},
			transition: ReviewTransition{
				Decision: ReviewDecisionRejected, DecidedBy: actor, DecidedAt: decidedAt,
			},
			want: ErrReviewConflict,
		},
		{
			name: "approved then assigned",
			state: ReviewTransitionState{Phase: PhaseAssigned, Review: Review{
				State: ReviewStateApproved, DecidedBy: &actor, DecidedAt: &decidedAt,
			}},
			transition: approved,
			want:       ErrReviewConflict,
		},
		{
			name:       "not required",
			state:      ReviewTransitionState{Phase: PhaseQueued, Review: Review{State: ReviewStateNotRequired}},
			transition: approved,
			want:       ErrReviewConflict,
		},
		{
			name:       "assigned",
			state:      ReviewTransitionState{Phase: PhaseAssigned, Review: Review{State: ReviewStatePending}},
			transition: approved,
			want:       ErrReviewConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateReviewTransition(test.state, test.transition)
			if !errors.Is(err, test.want) {
				t.Errorf("ValidateReviewTransition() error = %v, want %v", err, test.want)
			}
		})
	}
}
