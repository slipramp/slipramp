package promotion

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

type recordingPromotionRequestStore struct {
	acceptance                PromotionAcceptance
	record                    Promotion
	releasePromotionID        uuid.UUID
	releaseEvaluatorProcessID uuid.UUID
	releaseStatus             KargoResponseStatus
	err                       error
	releaseErr                error
}

type recordingPromotionGateNotifier struct {
	calls int
}

func (n *recordingPromotionGateNotifier) Notify() {
	n.calls++
}

func (s *recordingPromotionRequestStore) AcceptPromotionRequest(
	_ context.Context,
	acceptance PromotionAcceptance,
) (Promotion, error) {
	s.acceptance = acceptance

	return s.record, s.err
}

func (s *recordingPromotionRequestStore) ReleasePromotion(
	_ context.Context,
	promotionID uuid.UUID,
	evaluatorProcessID uuid.UUID,
) (KargoResponseStatus, error) {
	s.releasePromotionID = promotionID
	s.releaseEvaluatorProcessID = evaluatorProcessID

	return s.releaseStatus, s.releaseErr
}

func TestKargoRequestDigestIsStableAndCoversEveryFact(t *testing.T) {
	t.Parallel()

	request := validKargoRequest()
	first, err := request.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	second, err := request.Digest()
	if err != nil {
		t.Fatalf("repeated Digest() error = %v", err)
	}
	if first != second {
		t.Fatalf("repeated digest = %x, want %x", second, first)
	}

	changed := request
	changed.CandidateRevision = "a4cb21a88e1d0000000000000000000000000000"
	changedDigest, err := changed.Digest()
	if err != nil {
		t.Fatalf("changed Digest() error = %v", err)
	}
	if changedDigest == first {
		t.Fatal("changed request produced the original digest")
	}
}

func TestKargoRequestRejectsPartialRevisionAndCandidateBranch(t *testing.T) {
	t.Parallel()

	request := validKargoRequest()
	request.BaseRevision = "8b7f1d2"
	if _, err := request.Digest(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short revision error = %v, want ErrInvalid", err)
	}

	request = validKargoRequest()
	request.CandidateBranch = "feature/external-dns"
	if _, err := request.Digest(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("candidate branch error = %v, want ErrInvalid", err)
	}

	request = validKargoRequest()
	request.CandidateBranch = ""
	if _, err := request.Digest(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("partial Git context error = %v, want ErrInvalid", err)
	}
}

func TestKargoRequestAllowsOmittedGitContext(t *testing.T) {
	t.Parallel()

	request := validKargoRequest()
	request.BaseRevision = ""
	request.CandidateRevision = ""
	request.CandidateBranch = ""
	if _, err := request.Digest(); err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	context, err := request.GitContext()
	if err != nil || context != nil {
		t.Fatalf("GitContext() = %#v, %v, want nil", context, err)
	}
}

func TestRequestServiceSubmitsValidatedPromotionAcceptance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 0, 123456789, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1910")
	evaluatorProcessID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1912")
	principal := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1911"),
		Kind:        productidentity.KindServiceAccount,
		DisplayName: "Kargo production",
		Role:        productidentity.RoleOperator,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	store := &recordingPromotionRequestStore{
		releaseStatus: KargoResponseStatusGated,
		record: Promotion{
			ID:            promotionID,
			StageSnapshot: PromotionStageSnapshot{Gates: []string{"network-idle"}},
			Lifecycle:     PromotionLifecycle{State: LifecycleApproved},
		},
	}
	notifier := &recordingPromotionGateNotifier{}
	service := NewRequestService(
		IntegrationSet{
			"prod": {
				Repositories:               []string{"ops"},
				MaximumStageObservationAge: 5 * time.Minute,
				PendingReviewRetention:     8 * 24 * time.Hour,
			},
		},
		store,
		evaluatorProcessID,
		notifier,
	)
	service.newID = func() uuid.UUID { return promotionID }
	service.now = func() time.Time { return now }

	submission, err := service.SubmitKargoPromotion(t.Context(), "prod", validKargoRequest(), principal)
	if err != nil {
		t.Fatalf("SubmitKargoPromotion() error = %v", err)
	}
	if submission.Promotion.ID != promotionID || submission.Status != KargoResponseStatusGated ||
		store.acceptance.ID != promotionID ||
		store.acceptance.RequestedByPrincipalID != principal.ID ||
		store.acceptance.RequestDigest == (RequestDigest{}) ||
		store.acceptance.AcceptedAt.Nanosecond() != 123456000 {
		t.Errorf("acceptance = %#v, submission = %#v", store.acceptance, submission)
	}
	if store.releasePromotionID != promotionID || store.releaseEvaluatorProcessID != evaluatorProcessID {
		t.Errorf("Gate release = %s, %s", store.releasePromotionID, store.releaseEvaluatorProcessID)
	}
	if notifier.calls != 1 {
		t.Errorf("Gate evaluation notifications = %d, want 1", notifier.calls)
	}

	store.releaseStatus = KargoResponseStatus(LifecycleApproved)
	store.record.StageSnapshot.Gates = nil
	store.releasePromotionID = uuid.Nil
	submission, err = service.SubmitKargoPromotion(t.Context(), "prod", validKargoRequest(), principal)
	if err != nil || submission.Status != KargoResponseStatus(LifecycleApproved) || store.releasePromotionID != promotionID {
		t.Errorf("released submission = %#v, %v", submission, err)
	}
}

func validKargoRequest() KargoRequest {
	return KargoRequest{
		ExternalPromotionID: "external-dns-01jbe4q8",
		Project:             "kargo-core",
		Stage:               "external-dns-prod",
		BaseRevision:        "8b7f1d217a6b0000000000000000000000000000",
		CandidateRevision:   "f4cb21a88e1d0000000000000000000000000000",
		CandidateBranch:     "kargo/promotion/external-dns-01jbe4q8",
	}
}
