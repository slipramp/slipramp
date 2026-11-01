package run

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestCreateRunUsesPromotionSourceAndRequestingActor(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	revision := testRevision()
	revision.Catalog.Targets[0].Spec.Actions["sync"] = catalog.Action{
		Command: []string{"slipramp-kargo-tools", "prepare"},
	}
	service.catalogs = &recordingCatalogs{revision: revision}
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1310")
	idempotencyKey := promotionID.String()
	actor := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1311"),
		Kind:        productidentity.KindServiceAccount,
		DisplayName: "Kargo production",
		Role:        productidentity.RoleOperator,
	}

	created, err := service.CreateRun(context.Background(), CreateRequest{
		TargetID:          testTargetID,
		ActionName:        "sync",
		CatalogRevisionID: &revision.ID,
		Source: Source{
			Type:      SourceTypePromotion,
			Promotion: &PromotionRunSource{PromotionID: promotionID},
		},
		IdempotencyKey: &idempotencyKey,
		Actor:          &actor,
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.Source.Promotion == nil || created.Source.Promotion.PromotionID != promotionID ||
		created.CreatedBy == nil || created.CreatedBy.PrincipalID != actor.ID ||
		created.CatalogRevisionID != revision.ID {
		t.Errorf("Promotion preparation Run = %#v", created)
	}
}

func TestCreateRunUsesMCPSourceAndRequestingActor(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	actor := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1312"),
		Kind:        productidentity.KindServiceAccount,
		DisplayName: "Codex",
		Role:        productidentity.RoleOperator,
	}
	idempotencyKey := "mcp-network-preview"

	created, err := service.CreateRun(context.Background(), CreateRequest{
		TargetID:       testTargetID,
		ActionName:     "preview",
		Parameters:     map[string]any{"mode": "safe"},
		Source:         Source{Type: SourceTypeMCP},
		IdempotencyKey: &idempotencyKey,
		Actor:          &actor,
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.Source.Type != SourceTypeMCP || created.CreatedBy == nil ||
		created.CreatedBy.PrincipalID != actor.ID {
		t.Errorf("MCP Run = %#v", created)
	}
}

func TestCreateRunRejectsInvalidSourceMetadata(t *testing.T) {
	t.Parallel()

	deliveryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1304")
	revisionID := testRevisionID
	deliveryKey := deliveryID.String()
	base := CreateRequest{
		TargetID:          testTargetID,
		ActionName:        "preview",
		Parameters:        map[string]any{"mode": "safe"},
		CatalogRevisionID: &revisionID,
		Source: Source{
			Type: SourceTypeGit,
			Git:  &GitRunSource{TriggerName: "reconcile", DeliveryID: deliveryID},
		},
		IdempotencyKey: &deliveryKey,
	}

	tests := []struct {
		name      string
		configure func(*CreateRequest)
		want      string
	}{
		{
			name: "Git revision pin",
			configure: func(request *CreateRequest) {
				request.CatalogRevisionID = nil
			},
			want: "must pin a catalog revision",
		},
		{
			name: "Git delivery idempotency",
			configure: func(request *CreateRequest) {
				key := "another-delivery"
				request.IdempotencyKey = &key
			},
			want: "must use the delivery ID",
		},
		{
			name: "Git actor",
			configure: func(request *CreateRequest) {
				request.Actor = &productidentity.Principal{
					ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1305"),
					Kind:        productidentity.KindUser,
					DisplayName: "Ada Lovelace",
					Role:        productidentity.RoleOperator,
				}
			},
			want: "must not have an actor",
		},
		{
			name: "manual revision pin",
			configure: func(request *CreateRequest) {
				request.Source = Source{Type: SourceTypeManual}
				request.IdempotencyKey = nil
			},
			want: "manual, API, and MCP Runs cannot pin",
		},
		{
			name: "manual review requirement",
			configure: func(request *CreateRequest) {
				request.Source = Source{Type: SourceTypeManual}
				request.CatalogRevisionID = nil
				request.IdempotencyKey = nil
				request.ReviewRequired = true
			},
			want: "cannot require Git-trigger review",
		},
		{
			name: "Promotion missing actor",
			configure: func(request *CreateRequest) {
				request.Source = Source{
					Type:      SourceTypePromotion,
					Promotion: &PromotionRunSource{PromotionID: deliveryID},
				}
			},
			want: "require the requesting actor",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := base
			test.configure(&request)
			service, runs := testService(t)
			_, err := service.CreateRun(context.Background(), request)
			if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), test.want) {
				t.Errorf("CreateRun() error = %v, want %q", err, test.want)
			}
			if runs.calls != 0 {
				t.Errorf("persistence calls = %d, want 0", runs.calls)
			}
		})
	}
}
