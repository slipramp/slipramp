package promotion

import (
	"context"
	"errors"
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
	expiredAt       time.Time
	promotionPage   PromotionReviewPage
	promotionFilter PromotionListFilter
	promotionCalls  int
	promotionReadAt time.Time
}

func (s *reviewStore) GetPromotionStage(_ context.Context, id uuid.UUID) (Stage, error) {
	for _, stage := range s.stages {
		if stage.ID == id {
			return stage, nil
		}
	}

	return Stage{}, ErrStageNotFound
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
		if current.Lifecycle.State != LifecyclePending {
			return Promotion{}, Decision{}, ErrTransitionConflict
		}
		current.Lifecycle.State = LifecycleState(record.Decision)
		current.Lifecycle.DecidedAt = &record.DecidedAt
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
		if record.Lifecycle.State != LifecyclePending || record.Lifecycle.ExpiresAt == nil ||
			record.Lifecycle.ExpiresAt.After(at) {
			continue
		}
		record.Lifecycle.State = LifecycleExpired
		record.UpdatedAt = at
		s.promotions[index] = record
		expired = append(expired, record)
	}

	return expired, nil
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
	service := NewReviewService(reviewIntegrationSet(), store)
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
	withoutPreparation := NewReviewService(IntegrationSet{"prod": {ClusterName: "colo"}}, store)
	withoutPreparation.now = func() time.Time { return now }
	if _, err := withoutPreparation.UpdateStagePolicy(
		t.Context(),
		active.ID,
		PolicyUpdate{Mode: PolicyModeReviewRequired},
		principal,
	); !errors.Is(err, ErrReviewContextUnavailable) {
		t.Fatalf("unconfigured review-required UpdateStagePolicy() error = %v", err)
	}

	for _, edit := range []func(*Stage){
		func(stage *Stage) { stage.Application = "" },
		func(stage *Stage) { stage.RepositoryName = "removed-from-allowlist" },
	} {
		stage := active
		edit(&stage)
		store.stages[0] = stage
		_, err := service.UpdateStagePolicy(t.Context(), stage.ID, PolicyUpdate{Mode: PolicyModeReviewRequired}, principal)
		if !errors.Is(err, ErrReviewContextUnavailable) {
			t.Errorf("review-required policy for %#v: %v", stage, err)
		}
	}
	if _, err := service.UpdateStagePolicy(
		t.Context(),
		active.ID,
		PolicyUpdate{Mode: PolicyModeAutomatic},
		principal,
	); err != nil {
		t.Fatalf("unenriched automatic UpdateStagePolicy() error = %v", err)
	}
}

func TestReviewServiceExpiresBeforeReadingPromotionPage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)
	record := Promotion{
		ID:        uuid.New(),
		Lifecycle: PromotionLifecycle{State: LifecyclePending, ExpiresAt: new(now.Add(-time.Minute))},
	}
	expired := record
	expired.Lifecycle.State = LifecycleExpired
	store := &reviewStore{
		promotions:    []Promotion{record},
		promotionPage: PromotionReviewPage{Items: []Promotion{expired}, Total: 5},
	}
	service := NewReviewService(nil, store)
	service.now = func() time.Time { return now }
	filter := PromotionListFilter{
		RepositoryID:     new(uuid.New()),
		CommitSHA:        "0123456789abcdef0123456789abcdef01234567",
		Status:           LifecycleExpired,
		CreationDispatch: CreationDispatchPhaseFailed,
		KargoOutcome:     KargoPhaseErrored,
		Integration:      "prod",
		StageID:          new(uuid.New()),
		Cluster:          "colo",
		Project:          "kargo-core",
		Application:      "external-dns",
		Limit:            1,
		Offset:           2,
	}

	result, err := service.ListPromotions(t.Context(), filter)
	if err != nil {
		t.Fatalf("ListPromotions() error = %v", err)
	}
	if store.promotionCalls != 1 || store.promotionFilter != filter || !store.promotionReadAt.Equal(now) ||
		store.promotions[0].Lifecycle.State != LifecycleExpired {
		t.Errorf("Promotion page read = calls %d, filter %#v, expiry time %s",
			store.promotionCalls, store.promotionFilter, store.promotionReadAt)
	}
	if result.Total != 5 || result.Limit != 1 || result.Offset != 2 || len(result.Items) != 1 ||
		result.Items[0].Promotion.ID != record.ID ||
		result.Items[0].Promotion.Lifecycle.State != LifecycleExpired {
		t.Errorf("ListPromotions() = %#v", result)
	}
}

func TestReviewServiceRejectsCommitFilterWithoutRepository(t *testing.T) {
	t.Parallel()

	service := NewReviewService(nil, &reviewStore{})
	_, err := service.ListPromotions(t.Context(), PromotionListFilter{
		CommitSHA: "0123456789abcdef0123456789abcdef01234567",
		Limit:     DefaultListLimit,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("ListPromotions() error = %v, want invalid filter", err)
	}
}

func TestReviewServiceRejectsInvalidOperationalFilters(t *testing.T) {
	t.Parallel()

	service := NewReviewService(nil, &reviewStore{})
	for _, filter := range []PromotionListFilter{
		{CreationDispatch: CreationDispatchPhase("missing"), Limit: DefaultListLimit},
		{KargoOutcome: KargoPhase("Running"), Limit: DefaultListLimit},
	} {
		if _, err := service.ListPromotions(t.Context(), filter); !errors.Is(err, ErrInvalid) {
			t.Errorf("ListPromotions(%#v) error = %v, want invalid filter", filter, err)
		}
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
			Lifecycle: PromotionLifecycle{State: LifecyclePending},
			CreatedAt: now.Add(-time.Hour),
			UpdatedAt: now.Add(-time.Hour),
		}},
		decisions: map[uuid.UUID]Decision{},
	}
	service := NewReviewService(nil, store)
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
	if review.Promotion.Lifecycle.State != LifecycleApproved || review.Decision == nil ||
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

func reviewIntegrationSet() IntegrationSet {
	return IntegrationSet{
		"prod": {
			ClusterName:  "colo",
			Repositories: []string{"ops"},
			Preparation: PreparationConfig{
				Repository: "ops",
				Target:     "kargo-promotions",
				Action:     "prepare-review",
			},
		},
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
