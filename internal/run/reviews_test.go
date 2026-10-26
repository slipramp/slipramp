package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestDecideRunReviewNormalizesComment(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	actor := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
	}
	decidedAt := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	runs := &recordingRuns{reviewed: Run{ID: runID}}
	service := &Service{reviews: runs, now: func() time.Time { return decidedAt }}
	comment := "  Reviewed against change request 42.  \n"

	record, err := service.DecideRunReview(context.Background(), ReviewDecisionRequest{
		RunID: runID, Decision: ReviewDecisionApproved, Comment: &comment, Actor: &actor,
	})
	if err != nil {
		t.Fatalf("DecideRunReview() error = %v", err)
	}
	if record.ID != runID || runs.reviewTransition.Comment == nil ||
		*runs.reviewTransition.Comment != "Reviewed against change request 42." {
		t.Errorf("review transition = %#v", runs.reviewTransition)
	}
	if !runs.reviewTransition.DecidedAt.Equal(decidedAt) ||
		runs.reviewTransition.DecidedAt.Location() != time.UTC {
		t.Errorf("decided at = %s, want %s in UTC", runs.reviewTransition.DecidedAt, decidedAt)
	}
}

func TestDecideRunReviewRejectsLongComment(t *testing.T) {
	t.Parallel()

	comment := strings.Repeat("x", MaximumReviewCommentLength+1)
	actor := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
	}
	runs := &recordingRuns{}
	service := &Service{reviews: runs, now: time.Now}

	_, err := service.DecideRunReview(context.Background(), ReviewDecisionRequest{
		RunID: uuid.New(), Decision: ReviewDecisionApproved, Comment: &comment, Actor: &actor,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DecideRunReview() error = %v, want %v", err, ErrInvalidRequest)
	}
	if runs.reviewTransition.RunID != uuid.Nil {
		t.Errorf("review store received transition %#v", runs.reviewTransition)
	}
}

func TestValidateReviewTransition(t *testing.T) {
	t.Parallel()

	actor := productidentity.ActorSnapshot{
		PrincipalID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
	}
	otherActor := actor
	otherActor.PrincipalID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501")
	decidedAt := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	approved := ReviewTransition{Decision: ReviewDecisionApproved, DecidedBy: actor, DecidedAt: decidedAt}
	rejected := ReviewTransition{Decision: ReviewDecisionRejected, DecidedBy: actor, DecidedAt: decidedAt}
	comment := "Reviewed against change request 42."
	approvedWithComment := approved
	approvedWithComment.Comment = &comment

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
			name: "identical repeated decision with comment",
			state: ReviewTransitionState{Phase: PhaseQueued, Review: Review{
				State: ReviewStateApproved, DecidedBy: &actor, DecidedAt: &decidedAt, Comment: &comment,
			}},
			transition: approvedWithComment,
		},
		{
			name: "different repeated comment",
			state: ReviewTransitionState{Phase: PhaseQueued, Review: Review{
				State: ReviewStateApproved, DecidedBy: &actor, DecidedAt: &decidedAt, Comment: &comment,
			}},
			transition: approved,
			want:       ErrReviewConflict,
		},
		{
			name: "identical repeated rejection",
			state: ReviewTransitionState{Phase: PhaseFinished, Review: Review{
				State: ReviewStateRejected, DecidedBy: &actor, DecidedAt: &decidedAt,
			}},
			transition: rejected,
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
			name: "different deciding Principal",
			state: ReviewTransitionState{Phase: PhaseQueued, Review: Review{
				State: ReviewStateApproved, DecidedBy: &actor, DecidedAt: &decidedAt,
			}},
			transition: ReviewTransition{
				Decision: ReviewDecisionApproved, DecidedBy: otherActor, DecidedAt: decidedAt,
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
			name: "rejected without terminal transition",
			state: ReviewTransitionState{Phase: PhaseQueued, Review: Review{
				State: ReviewStateRejected, DecidedBy: &actor, DecidedAt: &decidedAt,
			}},
			transition: rejected,
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
