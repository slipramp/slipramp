package promotion

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestPolicyModeEffective(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	if got := PolicyModeReviewRequired.Effective(&future, now); got != PolicyModeReviewRequired {
		t.Errorf("Effective(future) = %q", got)
	}
	if got := PolicyModeReviewRequired.Effective(&past, now); got != PolicyModeAutomatic {
		t.Errorf("Effective(past) = %q", got)
	}
}

func TestPromotionValidateAutomaticApproval(t *testing.T) {
	t.Parallel()

	promotion := validPromotion()
	promotion.ReviewRequired = false
	promotion.Status = StatusApproved
	promotion.PreparationRepositoryID = nil
	promotion.PreparationCatalogRevisionID = nil
	promotion.PreparationRunID = nil
	promotion.GitDiffArtifactID = nil
	promotion.ArgoDiffArtifactID = nil
	promotion.PreparationCompletedAt = nil

	if err := promotion.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	promotion.Status = StatusPending
	if err := promotion.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate() error = %v, want ErrInvalid", err)
	}
}

func TestPromotionValidateCreatingOperatorPromotion(t *testing.T) {
	t.Parallel()

	record := validPromotion()
	sourceStageID := uuid.New()
	nextDispatchAt := record.CreatedAt
	record.Source = SourceSlipramp
	record.SourceStageID = &sourceStageID
	record.SelectedFreightSnapshot = &FreightItem{Origin: "Warehouse/app", Name: "freight-42", Artifacts: []FreightArtifact{}}
	record.RequestDigest = RequestDigest{}
	record.BaseRevision = ""
	record.CandidateRevision = ""
	record.CandidateBranch = ""
	record.Status = StatusCreating
	record.PreparationRepositoryID = nil
	record.PreparationCatalogRevisionID = nil
	record.PreparationRunID = nil
	record.GitDiffArtifactID = nil
	record.ArgoDiffArtifactID = nil
	record.PreparationCompletedAt = nil
	record.NextDispatchAt = &nextDispatchAt
	record.UpdatedAt = record.CreatedAt

	if err := record.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	dispatchedAt := record.CreatedAt.Add(time.Second)
	kargoUID := "promotion-uid"
	record.NextDispatchAt = nil
	record.DispatchedAt = &dispatchedAt
	record.KargoPromotionUID = &kargoUID
	record.NextReconciliationAt = &dispatchedAt
	record.UpdatedAt = dispatchedAt
	if err := record.Validate(); err != nil {
		t.Fatalf("dispatched Validate() error = %v", err)
	}
	record.NextDispatchAt = &nextDispatchAt
	if err := record.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("due and dispatched error = %v, want ErrInvalid", err)
	}
	record.NextDispatchAt = nil

	record.SourceStageID = nil
	if err := record.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing source Stage error = %v, want ErrInvalid", err)
	}
}

func TestStagePolicyRejectsExpiredUpdate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	principalID := uuid.New()
	policy := StagePolicy{
		StageID:              uuid.New(),
		Mode:                 PolicyModeReviewRequired,
		ExpiresAt:            new(now.Add(-time.Minute)),
		UpdatedByPrincipalID: principalID,
		UpdatedBySnapshot: productidentity.ActorSnapshot{
			PrincipalID: principalID,
			Kind:        productidentity.KindUser,
			DisplayName: "Operator",
		},
		UpdatedAt: now,
	}

	if err := policy.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate() error = %v, want ErrInvalid", err)
	}
}

func validPromotion() Promotion {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	principalID := uuid.New()
	runID := uuid.New()
	preparationRepositoryID := uuid.New()
	preparationRevisionID := uuid.New()
	gitArtifactID := uuid.New()
	argoArtifactID := uuid.New()
	stage := reviewStage("prod", "colo", "kargo-core", "external-dns-prod", now)

	return Promotion{
		ID:                           uuid.New(),
		Source:                       SourceKargo,
		Integration:                  "prod",
		ExternalPromotionID:          "external-dns-01jbe4q8",
		RequestDigest:                RequestDigest{1},
		StageID:                      stage.ID,
		StageSnapshot:                stage.PromotionSnapshot(),
		RepositoryID:                 uuid.New(),
		RepositorySnapshot:           PromotionRepositorySnapshot{Name: "ops", CloneURL: "https://example.com/ops.git", Branch: "main"},
		BaseRevision:                 "8b7f1d217a6b0000000000000000000000000000",
		CandidateRevision:            "f4cb21a88e1d0000000000000000000000000000",
		CandidateBranch:              "kargo/promotion/external-dns-01jbe4q8",
		TargetBranch:                 "main",
		PolicySnapshot:               PromotionPolicySnapshot{ConfiguredMode: PolicyModeReviewRequired, EffectiveMode: PolicyModeReviewRequired},
		ReviewRequired:               true,
		Status:                       StatusPending,
		PreparationRepositoryID:      &preparationRepositoryID,
		PreparationCatalogRevisionID: &preparationRevisionID,
		PreparationRunID:             &runID,
		GitDiffArtifactID:            &gitArtifactID,
		ArgoDiffArtifactID:           &argoArtifactID,
		RequestedByPrincipalID:       principalID,
		RequestedBySnapshot: productidentity.ActorSnapshot{
			PrincipalID: principalID,
			Kind:        productidentity.KindServiceAccount,
			DisplayName: "Kargo production",
		},
		CreatedAt:              now,
		PreparationCompletedAt: new(now.Add(time.Minute)),
		ExpiresAt:              new(now.Add(8 * 24 * time.Hour)),
		UpdatedAt:              now.Add(time.Minute),
	}
}
