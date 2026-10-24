package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

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

func TestCreationServiceBuildsOperatorAcceptance(t *testing.T) {
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

	record, err := service.Create(t.Context(), OperatorPromotionRequest{
		TargetStageID: stageID,
		SourceStageID: stageID,
		FreightName:   "freight-42",
		Principal:     principal,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if record.ID != promotionID || store.acceptance.ID != promotionID ||
		store.acceptance.Integration != "prod" || store.acceptance.FreightName != "freight-42" ||
		store.acceptance.RequestedByPrincipalID != principal.ID ||
		store.acceptance.AcceptedAt.Nanosecond() != 123456000 {
		t.Errorf("acceptance = %#v, record = %#v", store.acceptance, record)
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
		Freight: json.RawMessage(`{"items":[{
          "origin":"Warehouse/external-dns",
          "name":"freight-42",
          "artifacts":[{"kind":"image","version":"1.2.3"}]
        }]}`),
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
	var item FreightItem
	if err := json.Unmarshal(selected, &item); err != nil || item.Name != "freight-42" ||
		item.Origin != "Warehouse/external-dns" {
		t.Errorf("selected Freight = %#v, %v", item, err)
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
