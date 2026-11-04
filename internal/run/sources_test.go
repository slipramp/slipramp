package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestCreateRunUsesScheduleSourceWithoutActorOrRevisionPin(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	revision := testRevision()
	action := revision.Catalog.Targets[0].Spec.Actions["preview"]
	action.Triggers = []catalog.Trigger{{
		Name:       "nightly-preview",
		Schedule:   &catalog.ScheduleTrigger{Cron: "0 3 * * *"},
		Parameters: map[string]any{"mode": "safe"},
	}}
	revision.Catalog.Targets[0].Spec.Actions["preview"] = action
	service.catalogs = &recordingCatalogs{revision: revision}
	scheduledAt := time.Date(2026, time.November, 3, 3, 0, 0, 0, time.UTC)
	idempotencyKey := ScheduleOccurrenceIdempotencyKey(
		testTargetID,
		"preview",
		"nightly-preview",
		scheduledAt,
	)

	created, err := service.CreateRun(context.Background(), CreateRequest{
		TargetID:   testTargetID,
		ActionName: "preview",
		Parameters: map[string]any{"mode": "safe"},
		Source: Source{
			Type: SourceTypeSchedule,
			Schedule: &ScheduleRunSource{
				TriggerName: "nightly-preview",
				ScheduledAt: scheduledAt,
			},
		},
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.Source.Schedule == nil || created.Source.Schedule.TriggerName != "nightly-preview" ||
		!created.Source.Schedule.ScheduledAt.Equal(scheduledAt) || created.CreatedBy != nil ||
		created.CatalogRevisionID != revision.ID {
		t.Errorf("scheduled Run = %#v", created)
	}
}

func TestCreateRunRejectsStaleScheduledTriggerDefinition(t *testing.T) {
	t.Parallel()

	scheduledAt := time.Date(2026, time.November, 3, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		trigger    *catalog.Trigger
		wantTarget error
	}{
		{name: "removed schedule", wantTarget: ErrScheduleNotFound},
		{
			name: "changed cron",
			trigger: &catalog.Trigger{
				Name:       "nightly-preview",
				Schedule:   &catalog.ScheduleTrigger{Cron: "0 4 * * *"},
				Parameters: map[string]any{"mode": "safe"},
			},
			wantTarget: ErrScheduleNotFound,
		},
		{
			name: "changed parameters",
			trigger: &catalog.Trigger{
				Name:       "nightly-preview",
				Schedule:   &catalog.ScheduleTrigger{Cron: "0 3 * * *"},
				Parameters: map[string]any{"mode": "fast"},
			},
			wantTarget: ErrInvalidParameters,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, persisted := testService(t)
			revision := testRevision()
			action := revision.Catalog.Targets[0].Spec.Actions["preview"]
			if test.trigger != nil {
				action.Triggers = []catalog.Trigger{*test.trigger}
			}
			revision.Catalog.Targets[0].Spec.Actions["preview"] = action
			service.catalogs = &recordingCatalogs{revision: revision}
			idempotencyKey := ScheduleOccurrenceIdempotencyKey(
				testTargetID,
				"preview",
				"nightly-preview",
				scheduledAt,
			)

			_, err := service.CreateRun(context.Background(), CreateRequest{
				TargetID:   testTargetID,
				ActionName: "preview",
				Parameters: map[string]any{"mode": "safe"},
				Source: Source{
					Type: SourceTypeSchedule,
					Schedule: &ScheduleRunSource{
						TriggerName: "nightly-preview",
						ScheduledAt: scheduledAt,
					},
				},
				IdempotencyKey: &idempotencyKey,
			})
			if !errors.Is(err, test.wantTarget) {
				t.Errorf("CreateRun() error = %v, want %v", err, test.wantTarget)
			}
			if persisted.calls != 0 {
				t.Errorf("persistence calls = %d, want 0", persisted.calls)
			}
		})
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
			name: "scheduled actor",
			configure: func(request *CreateRequest) {
				scheduledAt := time.Date(2026, time.November, 3, 3, 0, 0, 0, time.UTC)
				key := ScheduleOccurrenceIdempotencyKey(
					request.TargetID,
					request.ActionName,
					"nightly-preview",
					scheduledAt,
				)
				request.Source = Source{
					Type: SourceTypeSchedule,
					Schedule: &ScheduleRunSource{
						TriggerName: "nightly-preview",
						ScheduledAt: scheduledAt,
					},
				}
				request.CatalogRevisionID = nil
				request.IdempotencyKey = &key
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
