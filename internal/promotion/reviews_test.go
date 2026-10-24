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
	stages     []Stage
	policies   map[uuid.UUID]StagePolicy
	promotions []Promotion
	decisions  map[uuid.UUID]Decision
}

func (s *reviewStore) ListPromotionStages(context.Context) ([]Stage, error) {
	return s.stages, nil
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

func (s *reviewStore) ListPromotions(context.Context) ([]Promotion, error) {
	return s.promotions, nil
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

func TestReviewServiceListsStageInventoryWithEffectivePolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)
	actor := reviewActor(t, now)
	active := reviewStage("prod", "colo", "kargo-core", "external-dns-prod", now)
	otherActive := reviewStage("prod", "colo", "kargo-core", "metrics-prod", now)
	archived := reviewStage("prod", "colo", "kargo-core", "metrics-prod", now)
	archived.ArchivedAt = new(now.Add(time.Minute))
	policyUpdatedAt := now.Add(-2 * time.Hour)
	expiresAt := now.Add(-time.Hour)
	store := &reviewStore{
		stages: []Stage{archived, otherActive, active},
		policies: map[uuid.UUID]StagePolicy{
			active.ID: {
				StageID:              active.ID,
				Mode:                 PolicyModeReviewRequired,
				ExpiresAt:            &expiresAt,
				UpdatedByPrincipalID: actor.PrincipalID,
				UpdatedBySnapshot:    actor,
				UpdatedAt:            policyUpdatedAt,
			},
		},
		promotions: []Promotion{
			{ID: uuid.New(), StageID: active.ID, Status: StatusPending},
			{ID: uuid.New(), StageID: active.ID, Status: StatusApproved},
		},
		decisions: map[uuid.UUID]Decision{},
	}
	service := NewReviewService(store)
	service.now = func() time.Time { return now }
	includeArchived := false

	result, err := service.ListStages(
		t.Context(),
		StageListFilter{
			Application: active.Application,
			Archived:    &includeArchived,
			Limit:       DefaultListLimit,
		},
	)
	if err != nil {
		t.Fatalf("ListStages() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("ListStages() = %#v", result)
	}
	item := result.Items[0]
	if item.Stage.ID != active.ID || item.PendingPromotionCount != 1 ||
		item.Policy.ConfiguredMode != PolicyModeReviewRequired ||
		item.Policy.EffectiveMode != PolicyModeAutomatic || item.Policy.UpdatedBy == nil {
		t.Errorf("Stage review = %#v", item)
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

func TestReviewServiceFiltersAndExpiresPromotionsBeforeReview(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)
	stageID := uuid.New()
	expiredAt := now.Add(-time.Minute)
	pendingID := uuid.New()
	otherID := uuid.New()
	repositoryID := uuid.New()
	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	store := &reviewStore{
		policies: map[uuid.UUID]StagePolicy{},
		promotions: []Promotion{
			{
				ID:            pendingID,
				RepositoryID:  repositoryID,
				BaseRevision:  commitSHA,
				Integration:   "prod",
				StageID:       stageID,
				StageSnapshot: []byte(`{"cluster":"colo","project":"kargo-core","application":"external-dns"}`),
				Status:        StatusPending,
				ExpiresAt:     &expiredAt,
				CreatedAt:     now.Add(-2 * time.Hour),
			},
			{
				ID:            otherID,
				RepositoryID:  uuid.New(),
				BaseRevision:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Integration:   "prod",
				StageID:       uuid.New(),
				StageSnapshot: []byte(`{"cluster":"colo","project":"kargo-core","application":"metrics"}`),
				Status:        StatusPending,
				ExpiresAt:     &expiredAt,
				CreatedAt:     now.Add(-time.Hour),
			},
		},
		decisions: map[uuid.UUID]Decision{},
	}
	service := NewReviewService(store)
	service.now = func() time.Time { return now }

	result, err := service.ListPromotions(
		t.Context(),
		PromotionListFilter{
			RepositoryID: &repositoryID,
			CommitSHA:    commitSHA,
			Status:       StatusExpired,
			Cluster:      "colo",
			Project:      "kargo-core",
			Application:  "external-dns",
			Limit:        DefaultListLimit,
		},
	)
	if err != nil {
		t.Fatalf("ListPromotions() error = %v", err)
	}
	if result.Total != 1 || result.Items[0].Promotion.ID != pendingID ||
		result.Items[0].Promotion.Status != StatusExpired {
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
		Freight:         []byte(`{"items":[]}`),
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
