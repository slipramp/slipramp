package promotion

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

type reviewStore struct {
	stages          []Stage
	policies        map[uuid.UUID]StagePolicy
	promotions      []Promotion
	decisions       map[uuid.UUID]Decision
	stagePage       StageReviewPage
	stageFilter     StageListFilter
	stageCalls      int
	expiredAt       time.Time
	promotionPage   PromotionReviewPage
	promotionFilter PromotionListFilter
	promotionCalls  int
	promotionReadAt time.Time
	pendingStageIDs []uuid.UUID
}

func (s *reviewStore) ListPromotionStages(context.Context) ([]Stage, error) {
	return s.stages, nil
}

func (s *reviewStore) ListPromotionStageReviews(_ context.Context, filter StageListFilter) (StageReviewPage, error) {
	s.stageFilter = filter
	s.stageCalls++

	return s.stagePage, nil
}

func (s *reviewStore) GetPromotionStage(_ context.Context, id uuid.UUID) (Stage, error) {
	for _, stage := range s.stages {
		if stage.ID == id {
			return stage, nil
		}
	}

	return Stage{}, ErrStageNotFound
}

func (s *reviewStore) GetPromotionStagePolicy(_ context.Context, id uuid.UUID) (StagePolicy, error) {
	record, ok := s.policies[id]
	if !ok {
		return StagePolicy{}, ErrPolicyNotFound
	}

	return record, nil
}

func (s *reviewStore) UpsertPromotionStagePolicy(
	_ context.Context,
	record StagePolicy,
) (StagePolicy, error) {
	if err := record.Validate(); err != nil {
		return StagePolicy{}, err
	}
	s.policies[record.StageID] = record

	return record, nil
}

func (s *reviewStore) ListPromotionReviews(_ context.Context, filter PromotionListFilter) (PromotionReviewPage, error) {
	s.promotionFilter = filter
	s.promotionCalls++
	s.promotionReadAt = s.expiredAt

	return s.promotionPage, nil
}

func (s *reviewStore) CountPendingPromotionsByStage(
	_ context.Context,
	stageIDs []uuid.UUID,
) (map[uuid.UUID]int, error) {
	s.pendingStageIDs = slices.Clone(stageIDs)
	counts := make(map[uuid.UUID]int)
	for _, record := range s.promotions {
		if record.Status == StatusPending && slices.Contains(stageIDs, record.StageID) {
			counts[record.StageID]++
		}
	}

	return counts, nil
}

func (s *reviewStore) GetPromotion(_ context.Context, id uuid.UUID) (Promotion, error) {
	for _, record := range s.promotions {
		if record.ID == id {
			return record, nil
		}
	}

	return Promotion{}, ErrPromotionNotFound
}

func (s *reviewStore) GetPromotionDecision(_ context.Context, id uuid.UUID) (Decision, error) {
	record, ok := s.decisions[id]
	if !ok {
		return Decision{}, ErrDecisionNotFound
	}

	return record, nil
}

func (s *reviewStore) DecidePromotion(
	_ context.Context,
	record Decision,
) (Promotion, Decision, error) {
	if err := record.Validate(); err != nil {
		return Promotion{}, Decision{}, err
	}
	for index, current := range s.promotions {
		if current.ID != record.PromotionID {
			continue
		}
		if current.Status != StatusPending {
			return Promotion{}, Decision{}, ErrTransitionConflict
		}
		current.Status = PromotionStatus(record.Decision)
		current.DecidedAt = &record.DecidedAt
		current.UpdatedAt = record.DecidedAt
		s.promotions[index] = current
		s.decisions[record.PromotionID] = record

		return current, record, nil
	}

	return Promotion{}, Decision{}, ErrPromotionNotFound
}

func (s *reviewStore) ExpirePendingPromotions(
	_ context.Context,
	at time.Time,
) ([]Promotion, error) {
	s.expiredAt = at
	var expired []Promotion
	for index, record := range s.promotions {
		if record.Status != StatusPending || record.ExpiresAt == nil || record.ExpiresAt.After(at) {
			continue
		}
		record.Status = StatusExpired
		record.UpdatedAt = at
		s.promotions[index] = record
		expired = append(expired, record)
	}

	return expired, nil
}

func TestReviewServiceForwardsStagePageAndEvaluatesPoliciesAtOneTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)
	actor := reviewActor(t, now)
	active := reviewStage("prod", "colo", "kargo-core", "external-dns-prod", now)
	expired := reviewStage("prod", "colo", "kargo-core", "metrics-prod", now)
	defaultPolicy := reviewStage("prod", "colo", "kargo-core", "cert-manager-prod", now)
	policyUpdatedAt := now.Add(-2 * time.Hour)
	expiresAt := now.Add(-time.Hour)
	store := &reviewStore{
		stagePage: StageReviewPage{
			Total: 17,
			Items: []StageReviewRecord{
				{
					Stage: active,
					Policy: &StagePolicy{
						StageID: active.ID, Mode: PolicyModeReviewRequired, ExpiresAt: new(now.Add(30 * time.Minute)),
						UpdatedByPrincipalID: actor.PrincipalID, UpdatedBySnapshot: actor, UpdatedAt: policyUpdatedAt,
					},
					PendingPromotionCount: 3,
				},
				{
					Stage: expired,
					Policy: &StagePolicy{
						StageID: expired.ID, Mode: PolicyModeReviewRequired, ExpiresAt: &expiresAt,
						UpdatedByPrincipalID: actor.PrincipalID, UpdatedBySnapshot: actor, UpdatedAt: policyUpdatedAt,
					},
				},
				{Stage: defaultPolicy},
			},
		},
		promotions: []Promotion{{ID: uuid.New(), Status: StatusPending, ExpiresAt: &now}},
	}
	service := NewReviewService(store)
	clockCalls := 0
	service.now = func() time.Time {
		clockCalls++
		return now.Add(time.Duration(clockCalls-1) * time.Hour)
	}
	includeArchived := false
	filter := StageListFilter{
		Integration: "prod", Cluster: "colo", Project: "kargo-core", Application: active.Application,
		Archived: &includeArchived, Limit: 3, Offset: 2,
	}

	result, err := service.ListStages(t.Context(), filter)
	if err != nil {
		t.Fatalf("ListStages() error = %v", err)
	}
	if store.stageFilter != filter || store.stageCalls != 1 ||
		result.Total != 17 || result.Limit != filter.Limit || result.Offset != filter.Offset || len(result.Items) != 3 {
		t.Fatalf("ListStages() = %#v", result)
	}
	item := result.Items[0]
	if item.Stage.ID != active.ID || item.PendingPromotionCount != 3 ||
		item.Policy.ConfiguredMode != PolicyModeReviewRequired ||
		item.Policy.EffectiveMode != PolicyModeReviewRequired || item.Policy.UpdatedBy == nil {
		t.Errorf("Stage review = %#v", item)
	}
	if result.Items[1].Stage.ID != expired.ID || result.Items[1].Policy.ConfiguredMode != PolicyModeReviewRequired ||
		result.Items[1].Policy.EffectiveMode != PolicyModeAutomatic ||
		result.Items[2].Stage.ID != defaultPolicy.ID || result.Items[2].Policy.ConfiguredMode != PolicyModeAutomatic ||
		result.Items[2].Policy.UpdatedBy != nil {
		t.Errorf("expired/default Stage policies = %#v", result.Items[1:])
	}
	if clockCalls != 1 || !store.expiredAt.Equal(now) || store.promotions[0].Status != StatusExpired {
		t.Errorf("evaluation times = %d clock calls, expired at %v, Promotions %#v", clockCalls, store.expiredAt, store.promotions)
	}
}

func TestReviewServiceRejectsArchivedPolicyUpdateAndAttributesValidUpdate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)
	principal := reviewPrincipal(now)
	active := reviewStage("prod", "colo", "kargo-core", "external-dns-prod", now)
	archived := reviewStage("prod", "colo", "kargo-core", "metrics-prod", now)
	archived.ArchivedAt = new(now)
	store := &reviewStore{
		stages:    []Stage{active, archived},
		policies:  map[uuid.UUID]StagePolicy{},
		decisions: map[uuid.UUID]Decision{},
	}
	service := NewReviewService(store)
	service.now = func() time.Time { return now }

	if _, err := service.UpdateStagePolicy(
		t.Context(),
		archived.ID,
		PolicyUpdate{Mode: PolicyModeAutomatic},
		principal,
	); !errors.Is(err, ErrStageArchived) {
		t.Fatalf("archived UpdateStagePolicy() error = %v", err)
	}

	expiresAt := now.Add(2 * time.Hour)
	reason := "Review the DNS migration"
	policy, err := service.UpdateStagePolicy(
		t.Context(),
		active.ID,
		PolicyUpdate{Mode: PolicyModeReviewRequired, ExpiresAt: &expiresAt, Reason: &reason},
		principal,
	)
	if err != nil {
		t.Fatalf("UpdateStagePolicy() error = %v", err)
	}
	if policy.EffectiveMode != PolicyModeReviewRequired || policy.UpdatedBy == nil ||
		policy.UpdatedBy.PrincipalID != principal.ID || policy.Reason == nil || *policy.Reason != reason {
		t.Errorf("updated policy = %#v", policy)
	}
}

func TestReviewServiceExpiresBeforeReadingPromotionPage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)
	record := Promotion{
		ID:        uuid.New(),
		Status:    StatusPending,
		ExpiresAt: new(now.Add(-time.Minute)),
	}
	expired := record
	expired.Status = StatusExpired
	store := &reviewStore{
		promotions:    []Promotion{record},
		promotionPage: PromotionReviewPage{Items: []Promotion{expired}, Total: 5},
	}
	service := NewReviewService(store)
	service.now = func() time.Time { return now }
	filter := PromotionListFilter{
		RepositoryID: new(uuid.New()),
		CommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		Status:       StatusExpired,
		Integration:  "prod",
		StageID:      new(uuid.New()),
		Cluster:      "colo",
		Project:      "kargo-core",
		Application:  "external-dns",
		Limit:        1,
		Offset:       2,
	}

	result, err := service.ListPromotions(t.Context(), filter)
	if err != nil {
		t.Fatalf("ListPromotions() error = %v", err)
	}
	if store.promotionCalls != 1 || store.promotionFilter != filter || !store.promotionReadAt.Equal(now) ||
		store.promotions[0].Status != StatusExpired {
		t.Errorf("Promotion page read = calls %d, filter %#v, expiry time %s",
			store.promotionCalls, store.promotionFilter, store.promotionReadAt)
	}
	if result.Total != 5 || result.Limit != 1 || result.Offset != 2 || len(result.Items) != 1 ||
		result.Items[0].Promotion.ID != record.ID || result.Items[0].Promotion.Status != StatusExpired {
		t.Errorf("ListPromotions() = %#v", result)
	}
}

func TestReviewServiceRejectsCommitFilterWithoutRepository(t *testing.T) {
	t.Parallel()

	service := NewReviewService(&reviewStore{})
	_, err := service.ListPromotions(t.Context(), PromotionListFilter{
		CommitSHA: "0123456789abcdef0123456789abcdef01234567",
		Limit:     DefaultListLimit,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("ListPromotions() error = %v, want invalid filter", err)
	}
}

func TestReviewServiceDecisionUsesAuthenticatedActor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)
	principal := reviewPrincipal(now)
	promotionID := uuid.New()
	store := &reviewStore{
		policies: map[uuid.UUID]StagePolicy{},
		promotions: []Promotion{{
			ID:        promotionID,
			Status:    StatusPending,
			CreatedAt: now.Add(-time.Hour),
			UpdatedAt: now.Add(-time.Hour),
		}},
		decisions: map[uuid.UUID]Decision{},
	}
	service := NewReviewService(store)
	service.now = func() time.Time { return now }
	comment := "Validated the RBAC changes"

	review, err := service.Decide(
		t.Context(),
		DecisionRequest{
			PromotionID: promotionID,
			Decision:    DecisionApproved,
			Comment:     &comment,
			Principal:   principal,
		},
	)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if review.Promotion.Status != StatusApproved || review.Decision == nil ||
		review.Decision.DecidedByPrincipalID != principal.ID ||
		review.Decision.DecidedBySnapshot.DisplayName != principal.DisplayName {
		t.Errorf("Decide() = %#v", review)
	}
}

func reviewStage(integration, cluster, project, name string, now time.Time) Stage {
	return Stage{
		ID:              uuid.New(),
		Integration:     integration,
		ClusterName:     cluster,
		Project:         project,
		Name:            name,
		KubernetesUID:   "uid-" + name,
		ResourceVersion: "42",
		Application:     name,
		Environment:     "prod",
		RepositoryName:  "ops",
		RepositoryURL:   "https://example.com/ops.git",
		TargetBranch:    "stage/" + name,
		SourcePaths:     []string{"k8s/" + name},
		Freight:         Freight{Items: []FreightItem{}},
		FirstObservedAt: now.Add(-time.Hour),
		LastObservedAt:  now,
	}
}

func reviewPrincipal(now time.Time) productidentity.Principal {
	return productidentity.Principal{
		ID:          uuid.New(),
		Kind:        productidentity.KindUser,
		DisplayName: "Review operator",
		Role:        productidentity.RoleOperator,
		CreatedAt:   now.Add(-24 * time.Hour),
		UpdatedAt:   now.Add(-24 * time.Hour),
	}
}

func reviewActor(t *testing.T, now time.Time) productidentity.ActorSnapshot {
	t.Helper()

	actor, err := reviewPrincipal(now).ActorSnapshot()
	if err != nil {
		t.Fatalf("ActorSnapshot() error = %v", err)
	}

	return actor
}
