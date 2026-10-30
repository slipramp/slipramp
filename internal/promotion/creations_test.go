package promotion

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type creationTestStore struct {
	stage      Stage
	acceptance OperatorPromotionAcceptance
	record     Promotion
	err        error
}

func (s *creationTestStore) GetPromotionStage(context.Context, uuid.UUID) (Stage, error) {
	return s.stage, s.err
}

func (s *creationTestStore) CreateOperatorPromotion(
	_ context.Context,
	acceptance OperatorPromotionAcceptance,
) (Promotion, error) {
	s.acceptance = acceptance

	return s.record, s.err
}

func TestCreationServiceBuildsDirectOperatorAcceptance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 123456789, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1930")
	stageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1931")
	principal := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1932"),
		Kind:        productidentity.KindUser,
		DisplayName: "Operator",
		Role:        productidentity.RoleOperator,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	store := &creationTestStore{
		stage:  Stage{ID: stageID, Integration: "prod"},
		record: Promotion{ID: promotionID},
	}
	service := NewCreationService(IntegrationSet{
		"prod": {
			MaximumStageObservationAge: 5 * time.Minute,
			PendingReviewRetention:     8 * 24 * time.Hour,
		},
	}, store)
	service.newID = func() uuid.UUID { return promotionID }
	service.now = func() time.Time { return now }
	policyExpiresAt := now.Add(30 * time.Minute)

	record, err := service.Create(t.Context(), OperatorPromotionRequest{
		TargetStageID: stageID,
		FreightName:   "freight-42",
		Policy: &PolicyUpdate{
			Mode:      PolicyModeReviewRequired,
			ExpiresAt: &policyExpiresAt,
		},
		Principal: principal,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if record.ID != promotionID || store.acceptance.ID != promotionID ||
		store.acceptance.Integration != "prod" || store.acceptance.FreightName != "freight-42" ||
		store.acceptance.SourceStageID != uuid.Nil ||
		store.acceptance.RequestedByPrincipalID != principal.ID ||
		store.acceptance.AcceptedAt.Nanosecond() != 123456000 || store.acceptance.Policy == nil ||
		store.acceptance.Policy.Mode != PolicyModeReviewRequired ||
		store.acceptance.Policy.ExpiresAt == nil || !store.acceptance.Policy.ExpiresAt.Equal(policyExpiresAt) ||
		store.acceptance.Policy.UpdatedByPrincipalID != principal.ID ||
		!store.acceptance.Policy.UpdatedAt.Equal(store.acceptance.AcceptedAt) {
		t.Errorf("acceptance = %#v, record = %#v", store.acceptance, record)
	}
}

func TestCreationExternalNamePrefixesStageAndPreservesPromotionID(t *testing.T) {
	t.Parallel()

	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1930")
	if name := CreationExternalName("external-dns-prod", promotionID); name !=
		"external-dns-prod-slipramp-"+promotionID.String() {
		t.Errorf("CreationExternalName() = %q", name)
	}

	longStageName := strings.Repeat("a.", 104) + "a"
	name := CreationExternalName(longStageName, promotionID)
	if len(name) != maximumKargoPromotionNameLength || !strings.HasSuffix(name, "-slipramp-"+promotionID.String()) {
		t.Errorf("long CreationExternalName() = %q (%d bytes)", name, len(name))
	}
	if err := kubevalidate.DNSSubdomain("generated Kargo Promotion name", name); err != nil {
		t.Errorf("long CreationExternalName() is invalid: %v", err)
	}
	request := validKargoRequest()
	request.ExternalPromotionID = name
	if _, err := request.Digest(); err != nil {
		t.Errorf("253-character callback identity is invalid: %v", err)
	}
	record := validPromotion()
	record.ExternalPromotionID = name
	if err := record.Validate(); err != nil {
		t.Errorf("253-character durable identity is invalid: %v", err)
	}
}

func TestSelectPromotionFreightValidatesUpstreamTopology(t *testing.T) {
	t.Parallel()

	source := Stage{
		ID:          uuid.New(),
		Integration: "prod",
		ClusterName: "colo",
		Project:     "kargo-core",
		Name:        "external-dns-qa",
		Freight: Freight{Items: []FreightItem{{
			Origin: "Warehouse/external-dns", Name: "freight-42",
			Artifacts: []FreightArtifact{{Kind: "image", Version: "1.2.3"}},
		}}},
	}
	target := Stage{
		ID:          uuid.New(),
		Integration: source.Integration,
		ClusterName: source.ClusterName,
		Project:     source.Project,
		Name:        "external-dns-prod",
		RequestedFreight: []FreightRequest{{
			Origin:       "Warehouse/external-dns",
			SourceStages: []string{source.Name},
		}},
	}

	selected, err := SelectPromotionFreight(target, source, "freight-42")
	if err != nil {
		t.Fatalf("SelectPromotionFreight() error = %v", err)
	}
	if selected.Name != "freight-42" || selected.Origin != "Warehouse/external-dns" {
		t.Errorf("selected Freight = %#v", selected)
	}
	source.Freight.Items[0].Artifacts[0].Version = "2.0.0"
	if selected.Artifacts[0].Version != "1.2.3" {
		t.Error("selected Freight changed with the observed source")
	}

	target.RequestedFreight[0].SourceStages = []string{"another-stage"}
	if _, err := SelectPromotionFreight(target, source, "freight-42"); !errors.Is(err, ErrFreightSource) {
		t.Fatalf("invalid source error = %v, want ErrFreightSource", err)
	}
	target.Project = "another-project"
	if _, err := SelectPromotionFreight(target, source, "freight-42"); !errors.Is(err, ErrStageScope) {
		t.Fatalf("cross-project error = %v, want ErrStageScope", err)
	}
	if _, err := SelectPromotionFreight(source, source, "freight-42"); err != nil {
		t.Fatalf("in-place SelectPromotionFreight() error = %v", err)
	}
	if _, err := SelectPromotionFreight(source, source, "missing"); !errors.Is(err, ErrFreightNotFound) {
		t.Fatalf("missing Freight error = %v, want ErrFreightNotFound", err)
	}
}

func TestSelectDirectPromotionFreightValidatesRequest(t *testing.T) {
	t.Parallel()

	target := Stage{
		RequestedFreight: []FreightRequest{{Origin: "Warehouse/app", Direct: true}},
		DirectFreight: Freight{Items: []FreightItem{{
			Origin: "Warehouse/app", Name: "freight-42",
			Artifacts: []FreightArtifact{{Kind: "image", Version: "1.2.3"}},
		}}},
	}

	selected, err := SelectDirectPromotionFreight(target, "freight-42")
	if err != nil {
		t.Fatalf("SelectDirectPromotionFreight() error = %v", err)
	}
	if selected.Name != "freight-42" || selected.Origin != "Warehouse/app" {
		t.Errorf("selected Freight = %#v", selected)
	}
	target.DirectFreight.Items[0].Artifacts[0].Version = "2.0.0"
	if selected.Artifacts[0].Version != "1.2.3" {
		t.Error("selected direct Freight changed with the observed Stage")
	}

	target.RequestedFreight[0].Direct = false
	if _, err := SelectDirectPromotionFreight(target, "freight-42"); !errors.Is(err, ErrFreightSource) {
		t.Fatalf("indirect request error = %v, want ErrFreightSource", err)
	}
	if _, err := SelectDirectPromotionFreight(target, "missing"); !errors.Is(err, ErrFreightNotFound) {
		t.Fatalf("missing Freight error = %v, want ErrFreightNotFound", err)
	}
}
