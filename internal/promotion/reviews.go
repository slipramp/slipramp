package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	Archived    *bool
	Limit       int
	Offset      int
}

type PromotionListFilter struct {
	Status      PromotionStatus
	Integration string
	Cluster     string
	Project     string
	StageID     *uuid.UUID
	Limit       int
	Offset      int
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
	ListPromotionStages(context.Context) ([]Stage, error)
	GetPromotionStage(context.Context, uuid.UUID) (Stage, error)
	GetPromotionStagePolicy(context.Context, uuid.UUID) (StagePolicy, error)
	UpsertPromotionStagePolicy(context.Context, StagePolicy) (StagePolicy, error)
	ListPromotions(context.Context) ([]Promotion, error)
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
	_, err := s.store.ExpirePendingPromotions(ctx, s.currentTime())

	return err
}

func (s *ReviewService) ListStages(
	ctx context.Context,
	filter StageListFilter,
) (StageReviewList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return StageReviewList{}, err
	}
	if err := s.ExpirePending(ctx); err != nil {
		return StageReviewList{}, err
	}

	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return StageReviewList{}, err
	}
	promotions, err := s.store.ListPromotions(ctx)
	if err != nil {
		return StageReviewList{}, err
	}
	pendingCounts := make(map[uuid.UUID]int)
	for _, record := range promotions {
		if record.Status == StatusPending {
			pendingCounts[record.StageID]++
		}
	}

	items := make([]StageReview, 0, len(stages))
	for _, stage := range stages {
		if !stageMatchesFilter(stage, filter) {
			continue
		}
		policy, policyErr := s.policy(ctx, stage.ID)
		if policyErr != nil {
			return StageReviewList{}, policyErr
		}
		items = append(items, StageReview{
			Stage:                 stage,
			Policy:                policy,
			PendingPromotionCount: pendingCounts[stage.ID],
		})
	}
	slices.SortFunc(items, func(left, right StageReview) int {
		if value := compareText(left.Stage.Integration, right.Stage.Integration); value != 0 {
			return value
		}
		if value := compareText(left.Stage.Project, right.Stage.Project); value != 0 {
			return value
		}

		return compareText(left.Stage.Name, right.Stage.Name)
	})

	total := len(items)
	items = page(items, filter.Offset, filter.Limit)

	return StageReviewList{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
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
	if err := s.ExpirePending(ctx); err != nil {
		return PromotionReviewList{}, err
	}

	records, err := s.store.ListPromotions(ctx)
	if err != nil {
		return PromotionReviewList{}, err
	}
	items := make([]PromotionReview, 0, len(records))
	for _, record := range records {
		matches, matchErr := promotionMatchesFilter(record, filter)
		if matchErr != nil {
			return PromotionReviewList{}, matchErr
		}
		if !matches {
			continue
		}
		items = append(items, PromotionReview{Promotion: record})
	}
	slices.SortFunc(items, func(left, right PromotionReview) int {
		if value := right.Promotion.CreatedAt.Compare(left.Promotion.CreatedAt); value != 0 {
			return value
		}

		return compareText(right.Promotion.ID.String(), left.Promotion.ID.String())
	})

	total := len(items)
	items = page(items, filter.Offset, filter.Limit)

	return PromotionReviewList{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
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

func (s *ReviewService) policy(ctx context.Context, stageID uuid.UUID) (PolicyView, error) {
	record, err := s.store.GetPromotionStagePolicy(ctx, stageID)
	if errors.Is(err, ErrPolicyNotFound) {
		return policyView(nil, s.currentTime()), nil
	}
	if err != nil {
		return PolicyView{}, err
	}

	return policyView(&record, s.currentTime()), nil
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

func stageMatchesFilter(stage Stage, filter StageListFilter) bool {
	if filter.Integration != "" && stage.Integration != filter.Integration {
		return false
	}
	if filter.Cluster != "" && stage.ClusterName != filter.Cluster {
		return false
	}
	if filter.Project != "" && stage.Project != filter.Project {
		return false
	}
	if filter.Archived != nil && (stage.ArchivedAt != nil) != *filter.Archived {
		return false
	}

	return true
}

func promotionMatchesFilter(record Promotion, filter PromotionListFilter) (bool, error) {
	if filter.Status != "" && record.Status != filter.Status {
		return false, nil
	}
	if filter.Integration != "" && record.Integration != filter.Integration {
		return false, nil
	}
	if filter.StageID != nil && record.StageID != *filter.StageID {
		return false, nil
	}
	if filter.Cluster == "" && filter.Project == "" {
		return true, nil
	}

	var snapshot PromotionStageSnapshot
	if err := json.Unmarshal(record.StageSnapshot, &snapshot); err != nil {
		return false, fmt.Errorf("decode Promotion Stage snapshot for filtering: %w", err)
	}
	if filter.Cluster != "" && snapshot.ClusterName != filter.Cluster {
		return false, nil
	}
	if filter.Project != "" && snapshot.Project != filter.Project {
		return false, nil
	}

	return true, nil
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
