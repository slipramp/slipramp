package run

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/repository"
)

func TestCreateRunFreezesResolvedExecution(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	createdAt := time.Date(2026, time.October, 16, 12, 5, 0, 0, time.UTC)
	service.newID = func() uuid.UUID { return runID }
	service.now = func() time.Time { return createdAt }
	idempotencyKey := "manual-network-preview"
	actor := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
		Role:        productidentity.RoleOperator,
	}

	created, err := service.CreateRun(
		context.Background(),
		CreateRequest{
			TargetID:   testTargetID,
			ActionName: "preview",
			Parameters: map[string]any{
				"mode":    "safe",
				"refresh": true,
				"retries": 3,
				"message": "preview",
			},
			Source:         Source{Type: SourceTypeManual},
			IdempotencyKey: &idempotencyKey,
			Actor:          &actor,
		},
	)
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if created.ID != runID || created.Phase != PhaseQueued ||
		created.RepositoryID != testRepositoryID ||
		created.CatalogRevisionID != testRevisionID ||
		created.CommitSHA != testCommitSHA ||
		created.TargetName != "network" || created.ActionName != "preview" ||
		created.PlacementName != "infrastructure" ||
		created.ClusterName != "colo" ||
		created.Source.Type != SourceTypeManual ||
		!created.CreatedAt.Equal(createdAt) {
		t.Errorf("created Run = %#v", created)
	}
	if created.RequestDigest == "" || runs.created.RequestDigest == "" {
		t.Error("request digest was not recorded")
	}
	wantParameters := map[string]any{
		"mode":    "safe",
		"refresh": true,
		"retries": float64(3),
		"message": "preview",
	}
	if !reflect.DeepEqual(created.Parameters, wantParameters) {
		t.Errorf("normalized parameters = %#v, want %#v", created.Parameters, wantParameters)
	}
	if created.CreatedBy == nil || created.CreatedBy.PrincipalID != actor.ID ||
		created.CreatedBy.DisplayName != actor.DisplayName ||
		runs.idempotencyPrincipalID == nil ||
		*runs.idempotencyPrincipalID != actor.ID {
		t.Errorf("Run attribution = %#v, idempotency principal = %v", created.CreatedBy, runs.idempotencyPrincipalID)
	}
	if created.Snapshot.Repository.CloneURL !=
		"https://github.com/example/infrastructure.git" ||
		created.Snapshot.Runtime.Spec.Image != "hashicorp/terraform:1.14.3" ||
		created.Snapshot.Action.Command[1] != "plan" ||
		created.Snapshot.Action.Timeout != "30m" ||
		created.Snapshot.Placement.JobNamespace != "slipramp-jobs" ||
		created.Snapshot.Placement.Credentials["backend"].SecretName !=
			"terraform-backend" {
		t.Errorf("frozen snapshot = %#v", created.Snapshot)
	}
}

func TestCreateRunFreezesTargetRelevanceSeparatelyFromCheckout(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	desiredCommitSHA := strings.Repeat("b", 40)
	service.catalogs = &recordingCatalogs{
		revision:         testRevision(),
		desiredCommitSHA: desiredCommitSHA,
	}

	created, err := service.CreateRun(context.Background(), CreateRequest{
		TargetID:   testTargetID,
		ActionName: "preview",
		Parameters: map[string]any{"mode": "safe"},
		Source:     Source{Type: SourceTypeManual},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.CommitSHA != testCommitSHA || created.TargetRelevanceCommitSHA == nil ||
		*created.TargetRelevanceCommitSHA != desiredCommitSHA {
		t.Errorf(
			"Run commits = checkout %q, relevance %v",
			created.CommitSHA,
			created.TargetRelevanceCommitSHA,
		)
	}
}

func TestCreateRunRejectsInvalidParametersBeforePersistence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parameters map[string]any
		want       string
	}{
		{
			name:       "required missing",
			parameters: map[string]any{"refresh": true},
			want:       "required parameter \"mode\" was not supplied",
		},
		{
			name:       "unknown",
			parameters: map[string]any{"mode": "safe", "other": "value"},
			want:       "parameter \"other\" is not declared",
		},
		{
			name:       "wrong type",
			parameters: map[string]any{"mode": "safe", "refresh": "true"},
			want:       "parameter \"refresh\" must be a boolean",
		},
		{
			name:       "enum outside values",
			parameters: map[string]any{"mode": "dangerous"},
			want:       "parameter \"mode\" must be one of safe, fast",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, runs := testService(t)
			_, err := service.CreateRun(
				context.Background(),
				CreateRequest{
					TargetID:   testTargetID,
					ActionName: "preview",
					Parameters: test.parameters,
					Source:     Source{Type: SourceTypeAPI},
				},
			)
			if !errors.Is(err, ErrInvalidParameters) ||
				!contains(err, test.want) {
				t.Errorf("CreateRun() error = %v, want %q", err, test.want)
			}
			if runs.calls != 0 {
				t.Errorf("persistence calls = %d, want 0", runs.calls)
			}
		})
	}
}

func TestCreateRunMapsMissingResolutionInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*Service)
		request   CreateRequest
		want      error
	}{
		{
			name: "repository",
			configure: func(service *Service) {
				service.repositories = &recordingRepositories{err: repository.ErrNotFound}
			},
			want: ErrRepositoryNotFound,
		},
		{
			name: "catalog",
			configure: func(service *Service) {
				service.catalogs = &recordingCatalogs{err: catalog.ErrNotFound}
			},
			want: ErrCatalogNotFound,
		},
		{
			name: "target",
			configure: func(service *Service) {
				service.catalogs = &recordingCatalogs{targetErr: catalog.ErrTargetNotFound}
			},
			want: ErrTargetNotFound,
		},
		{
			name:    "action",
			request: CreateRequest{ActionName: "missing"},
			want:    ErrActionNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, _ := testService(t)
			if test.configure != nil {
				test.configure(service)
			}
			request := CreateRequest{
				TargetID:   testTargetID,
				ActionName: "preview",
				Parameters: map[string]any{"mode": "safe"},
				Source:     Source{Type: SourceTypeAPI},
			}
			if test.request.ActionName != "" {
				request.ActionName = test.request.ActionName
			}

			_, err := service.CreateRun(context.Background(), request)
			if !errors.Is(err, test.want) {
				t.Errorf("CreateRun() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCreateRunUsesExplicitCatalogRevisionPin(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	pinnedRevision := testRevision()
	pinnedRevision.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1302")
	pinnedRevision.CommitSHA = strings.Repeat("b", 40)
	pinnedRevision.Catalog.Targets[0].Spec.Actions["sync"] = catalog.Action{
		Command: []string{"terraform", "apply"},
	}
	activeRevision := testRevision()
	activeRevision.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1303")
	activeRevision.CommitSHA = strings.Repeat("c", 40)
	catalogs := &pinnedCatalogs{active: activeRevision, pinned: pinnedRevision}
	service.catalogs = catalogs
	deliveryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1304")
	idempotencyKey := deliveryID.String()

	created, err := service.CreateRun(context.Background(), CreateRequest{
		TargetID:          testTargetID,
		ActionName:        "sync",
		CatalogRevisionID: &pinnedRevision.ID,
		Source: Source{
			Type: SourceTypeGit,
			Git:  &GitRunSource{TriggerName: "reconcile", DeliveryID: deliveryID},
		},
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if catalogs.activeCalls != 0 || catalogs.pinnedCalls != 1 {
		t.Errorf("catalog calls = active %d, pinned %d", catalogs.activeCalls, catalogs.pinnedCalls)
	}
	if created.CatalogRevisionID != pinnedRevision.ID || created.CommitSHA != pinnedRevision.CommitSHA {
		t.Errorf("created Run pin = (%s, %s)", created.CatalogRevisionID, created.CommitSHA)
	}
}
