package promotion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	DefaultListLimit = 50
	MaximumListLimit = 200
)

type StageListFilter struct {
	Integration string
	Cluster     string
	Project     string
	Application string
	Archived    *bool
	Limit       int
	Offset      int
}

type PromotionListFilter struct {
	RepositoryID *uuid.UUID
	CommitSHA    string
	Status       PromotionStatus
	Integration  string
	Cluster      string
	Project      string
	Application  string
	StageID      *uuid.UUID
	Limit        int
	Offset       int
}

type StageReview struct {
	Stage                 Stage
	Policy                PolicyView
	PendingPromotionCount int
}

type PolicyView struct {
	ConfiguredMode PolicyMode
	EffectiveMode  PolicyMode
	ExpiresAt      *time.Time
	Reason         *string
	UpdatedBy      *productidentity.ActorSnapshot
	UpdatedAt      *time.Time
}

type StageReviewList struct {
	Items  []StageReview
	Total  int
	Limit  int
	Offset int
}

type PromotionReview struct {
	Promotion Promotion
	Decision  *Decision
}

type PromotionReviewList struct {
	Items  []PromotionReview
	Total  int
	Limit  int
	Offset int
}

type PromotionReviewPage struct {
	Items []Promotion
	Total int
}

type PolicyUpdate struct {
	Mode      PolicyMode
	ExpiresAt *time.Time
	Reason    *string
}

type DecisionRequest struct {
	PromotionID uuid.UUID
	Decision    DecisionValue
	Comment     *string
	Principal   productidentity.Principal
}

type ReviewStore interface {
	StageReviewStore
	GetPromotionStage(context.Context, uuid.UUID) (Stage, error)
	UpsertPromotionStagePolicy(context.Context, StagePolicy) (StagePolicy, error)
	ListPromotionReviews(context.Context, PromotionListFilter) (PromotionReviewPage, error)
	GetPromotion(context.Context, uuid.UUID) (Promotion, error)
	GetPromotionDecision(context.Context, uuid.UUID) (Decision, error)
	DecidePromotion(context.Context, Decision) (Promotion, Decision, error)
	ExpirePendingPromotions(context.Context, time.Time) ([]Promotion, error)
}

type ReviewService struct {
	store ReviewStore
	now   func() time.Time
}

func NewReviewService(store ReviewStore) *ReviewService {
	return &ReviewService{store: store, now: time.Now}
}

func (s *ReviewService) ExpirePending(ctx context.Context) error {
	return s.ExpirePendingAt(ctx, s.currentTime())
}

func (s *ReviewService) ExpirePendingAt(ctx context.Context, at time.Time) error {
	_, err := s.store.ExpirePendingPromotions(ctx, at.UTC().Truncate(time.Microsecond))

	return err
}

func (s *ReviewService) UpdateStagePolicy(
	ctx context.Context,
	stageID uuid.UUID,
	update PolicyUpdate,
	principal productidentity.Principal,
) (PolicyView, error) {
	stage, err := s.store.GetPromotionStage(ctx, stageID)
	if err != nil {
		return PolicyView{}, err
	}
	if stage.ArchivedAt != nil {
		return PolicyView{}, ErrStageArchived
	}
	actor, err := principal.ActorSnapshot()
	if err != nil {
		return PolicyView{}, fmt.Errorf("%w: policy actor is invalid: %w", ErrInvalid, err)
	}
	now := s.currentTime()
	record := StagePolicy{
		StageID:              stageID,
		Mode:                 update.Mode,
		ExpiresAt:            update.ExpiresAt,
		Reason:               update.Reason,
		UpdatedByPrincipalID: principal.ID,
		UpdatedBySnapshot:    actor,
		UpdatedAt:            now,
	}
	stored, err := s.store.UpsertPromotionStagePolicy(ctx, record)
	if err != nil {
		return PolicyView{}, err
	}

	return policyView(&stored, now), nil
}

func (s *ReviewService) ListPromotions(
	ctx context.Context,
	filter PromotionListFilter,
) (PromotionReviewList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return PromotionReviewList{}, err
	}
	if filter.Status != "" && !filter.Status.Valid() {
		return PromotionReviewList{}, fmt.Errorf("%w: Promotion status filter is invalid", ErrInvalid)
	}
	if filter.RepositoryID != nil && *filter.RepositoryID == uuid.Nil {
		return PromotionReviewList{}, fmt.Errorf("%w: repository ID must not be empty", ErrInvalid)
	}
	if filter.CommitSHA != "" {
		if err := validateRevision("commit SHA filter", filter.CommitSHA); err != nil {
			return PromotionReviewList{}, err
		}
		if filter.RepositoryID == nil {
			return PromotionReviewList{}, fmt.Errorf("%w: commit filter requires a repository ID", ErrInvalid)
		}
	}
	if err := s.ExpirePending(ctx); err != nil {
		return PromotionReviewList{}, err
	}

	page, err := s.store.ListPromotionReviews(ctx, filter)
	if err != nil {
		return PromotionReviewList{}, err
	}
	items := make([]PromotionReview, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, PromotionReview{Promotion: record})
	}

	return PromotionReviewList{Items: items, Total: page.Total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *ReviewService) GetPromotion(
	ctx context.Context,
	promotionID uuid.UUID,
) (PromotionReview, error) {
	if err := s.ExpirePending(ctx); err != nil {
		return PromotionReview{}, err
	}
	record, err := s.store.GetPromotion(ctx, promotionID)
	if err != nil {
		return PromotionReview{}, err
	}

	review := PromotionReview{Promotion: record}
	decision, err := s.store.GetPromotionDecision(ctx, promotionID)
	if err == nil {
		review.Decision = &decision
	} else if err != nil && !errors.Is(err, ErrDecisionNotFound) {
		return PromotionReview{}, err
	}

	return review, nil
}

func (s *ReviewService) Decide(
	ctx context.Context,
	request DecisionRequest,
) (PromotionReview, error) {
	actor, err := request.Principal.ActorSnapshot()
	if err != nil {
		return PromotionReview{}, fmt.Errorf("%w: decision actor is invalid: %w", ErrInvalid, err)
	}
	record := Decision{
		PromotionID:          request.PromotionID,
		Decision:             request.Decision,
		Comment:              request.Comment,
		DecidedByPrincipalID: request.Principal.ID,
		DecidedBySnapshot:    actor,
		DecidedAt:            s.currentTime(),
	}
	updated, decision, err := s.store.DecidePromotion(ctx, record)
	if err != nil {
		return PromotionReview{}, err
	}

	return PromotionReview{Promotion: updated, Decision: &decision}, nil
}

func (s *ReviewService) currentTime() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}

func policyView(record *StagePolicy, at time.Time) PolicyView {
	if record == nil {
		return PolicyView{
			ConfiguredMode: PolicyModeAutomatic,
			EffectiveMode:  PolicyModeAutomatic,
		}
	}

	updatedBy := record.UpdatedBySnapshot
	updatedAt := record.UpdatedAt

	return PolicyView{
		ConfiguredMode: record.Mode,
		EffectiveMode:  record.Mode.Effective(record.ExpiresAt, at),
		ExpiresAt:      record.ExpiresAt,
		Reason:         record.Reason,
		UpdatedBy:      &updatedBy,
		UpdatedAt:      &updatedAt,
	}
}

func validateListWindow(limit, offset int) error {
	if limit < 1 || limit > MaximumListLimit {
		return fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalid, MaximumListLimit)
	}
	if offset < 0 {
		return fmt.Errorf("%w: offset must not be negative", ErrInvalid)
	}

	return nil
}

func page[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := min(offset+limit, len(items))

	return items[offset:end]
}

func compareText(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}

	return 0
}
