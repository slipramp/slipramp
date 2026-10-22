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
	acceptance PromotionAcceptance
	record     Promotion
	err        error
}

func (s *recordingPromotionRequestStore) AcceptPromotionRequest(
	_ context.Context,
	acceptance PromotionAcceptance,
) (Promotion, error) {
	s.acceptance = acceptance

	return s.record, s.err
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
}

func TestRequestServiceSubmitsValidatedPromotionAcceptance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 0, 123456789, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1910")
	principal := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1911"),
		Kind:        productidentity.KindServiceAccount,
		DisplayName: "Kargo production",
		Role:        productidentity.RoleOperator,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	store := &recordingPromotionRequestStore{
		record: Promotion{ID: promotionID},
	}
	service := NewRequestService(
		IntegrationSet{
			"prod": {
				Repositories:               []string{"ops"},
				MaximumStageObservationAge: 5 * time.Minute,
				PendingReviewRetention:     8 * 24 * time.Hour,
			},
		},
		store,
	)
	service.newID = func() uuid.UUID { return promotionID }
	service.now = func() time.Time { return now }

	created, err := service.SubmitKargoPromotion(t.Context(), "prod", validKargoRequest(), principal)
	if err != nil {
		t.Fatalf("SubmitKargoPromotion() error = %v", err)
	}
	if created.ID != promotionID || store.acceptance.ID != promotionID ||
		store.acceptance.RequestedByPrincipalID != principal.ID ||
		store.acceptance.RequestDigest == (RequestDigest{}) ||
		store.acceptance.AcceptedAt.Nanosecond() != 123456000 {
		t.Errorf("acceptance = %#v, created = %#v", store.acceptance, created)
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
