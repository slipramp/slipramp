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
	"github.com/slipramp/slipramp/internal/placement"
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
			RepositoryID: testRepositoryID,
			TargetName:   "network",
			ActionName:   "preview",
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

func TestRerunRunExactCopiesFrozenExecution(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	original := finishedTestRun(t)
	runs.loaded = original
	newID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1301")
	createdAt := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	service.newID = func() uuid.UUID { return newID }
	service.now = func() time.Time { return createdAt }
	service.catalogs = &recordingCatalogs{err: errors.New("exact rerun must not resolve the catalog")}
	actor := productidentity.Principal{
		ID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500"),
		Kind:        productidentity.KindUser,
		DisplayName: "Ada Lovelace",
		Role:        productidentity.RoleOperator,
	}
	idempotencyKey := "exact-rerun"

	created, err := service.RerunRun(
		context.Background(),
		RerunRequest{
			RunID:          original.ID,
			Mode:           RerunModeExact,
			Source:         Source{Type: SourceTypeManual},
			IdempotencyKey: &idempotencyKey,
			Actor:          &actor,
		},
	)
	if err != nil {
		t.Fatalf("RerunRun() error = %v", err)
	}

	if created.ID != newID || created.Phase != PhaseQueued || created.Outcome != nil ||
		created.CatalogRevisionID != original.CatalogRevisionID || created.CommitSHA != original.CommitSHA ||
		created.ClusterName != original.ClusterName || created.PlacementName != original.PlacementName ||
		!created.CreatedAt.Equal(createdAt) {
		t.Errorf("exact rerun = %#v", created)
	}
	if created.RerunOfRunID == nil || *created.RerunOfRunID != original.ID ||
		created.RerunMode == nil || *created.RerunMode != RerunModeExact {
		t.Errorf("rerun provenance = (%v, %v)", created.RerunOfRunID, created.RerunMode)
	}
	if created.Source.Type != SourceTypeManual || created.CreatedBy == nil ||
		created.CreatedBy.PrincipalID != actor.ID {
		t.Errorf("rerun attribution = (%#v, %#v)", created.Source, created.CreatedBy)
	}
	if !reflect.DeepEqual(created.Parameters, original.Parameters) ||
		!reflect.DeepEqual(created.Snapshot, original.Snapshot) {
		t.Errorf("exact rerun did not preserve frozen execution")
	}
}

func TestRerunRunLatestResolvesActiveExecution(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	original := finishedTestRun(t)
	runs.loaded = original
	latest := testRevision()
	latest.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1302")
	latest.CommitSHA = "abcdef0123456789abcdef0123456789abcdef01"
	latest.Catalog.Runtimes[0].Spec.Image = "hashicorp/terraform:1.15.0"
	service.catalogs = &recordingCatalogs{revision: latest}

	created, err := service.RerunRun(
		context.Background(),
		RerunRequest{
			RunID:  original.ID,
			Mode:   RerunModeLatest,
			Source: Source{Type: SourceTypeAPI},
		},
	)
	if err != nil {
		t.Fatalf("RerunRun() error = %v", err)
	}

	if created.CatalogRevisionID != latest.ID || created.CommitSHA != latest.CommitSHA ||
		created.Snapshot.Runtime.Spec.Image != "hashicorp/terraform:1.15.0" {
		t.Errorf("latest rerun resolution = %#v", created)
	}
	if created.RerunOfRunID == nil || *created.RerunOfRunID != original.ID ||
		created.RerunMode == nil || *created.RerunMode != RerunModeLatest {
		t.Errorf("rerun provenance = (%v, %v)", created.RerunOfRunID, created.RerunMode)
	}
}

func TestRerunRunRejectsUnfinishedAndLatestArtifactRuns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*Run)
		want      error
	}{
		{
			name: "unfinished",
			configure: func(record *Run) {
				record.Phase = PhaseRunning
				record.Outcome = nil
			},
			want: ErrRunNotFinished,
		},
		{
			name: "artifact inputs",
			configure: func(record *Run) {
				record.Snapshot.Inputs["plan"] = ArtifactInput{ArtifactID: uuid.New()}
			},
			want: ErrLatestRerunInputs,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, runs := testService(t)
			original := finishedTestRun(t)
			test.configure(&original)
			runs.loaded = original

			_, err := service.RerunRun(
				context.Background(),
				RerunRequest{
					RunID:  original.ID,
					Mode:   RerunModeLatest,
					Source: Source{Type: SourceTypeManual},
				},
			)
			if !errors.Is(err, test.want) {
				t.Errorf("RerunRun() error = %v, want %v", err, test.want)
			}
			if runs.calls != 0 {
				t.Errorf("persistence calls = %d, want 0", runs.calls)
			}
		})
	}
}

func finishedTestRun(t *testing.T) Run {
	t.Helper()

	revision := testRevision()
	target := revision.Catalog.Targets[0]
	action, found := target.EffectiveAction("preview")
	if !found {
		t.Fatal("test action is missing")
	}
	outcome := OutcomeFailed

	return Run{
		ID:                uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300"),
		Phase:             PhaseFinished,
		Outcome:           &outcome,
		RepositoryID:      testRepositoryID,
		CatalogRevisionID: revision.ID,
		CommitSHA:         revision.CommitSHA,
		TargetName:        "network",
		ActionName:        "preview",
		Parameters:        map[string]any{"mode": "safe", "refresh": true},
		PlacementName:     "infrastructure",
		ClusterName:       "colo",
		Source:            Source{Type: SourceTypeAPI},
		RequestDigest:     strings.Repeat("a", 64),
		Snapshot: Snapshot{
			Repository: RepositorySnapshot{
				Name:     "infrastructure",
				CloneURL: "https://github.com/example/infrastructure.git",
				Branch:   "main",
			},
			Target:    target,
			Action:    action,
			Runtime:   revision.Catalog.Runtimes[0],
			Placement: PlacementSnapshot{Name: "infrastructure", ClusterName: "colo"},
			Inputs:    map[string]ArtifactInput{},
		},
		CreatedAt: time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC),
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
					RepositoryID: testRepositoryID,
					TargetName:   "network",
					ActionName:   "preview",
					Parameters:   test.parameters,
					Source:       Source{Type: SourceTypeAPI},
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
			name:    "target",
			request: CreateRequest{TargetName: "missing"},
			want:    ErrTargetNotFound,
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
				RepositoryID: testRepositoryID,
				TargetName:   "network",
				ActionName:   "preview",
				Parameters:   map[string]any{"mode": "safe"},
				Source:       Source{Type: SourceTypeAPI},
			}
			if test.request.TargetName != "" {
				request.TargetName = test.request.TargetName
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

func TestCreateRunReturnsIdempotentRunBeforeCatalogResolution(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	idempotencyKey := "manual-network-preview"
	request := CreateRequest{
		RepositoryID:   testRepositoryID,
		TargetName:     "network",
		ActionName:     "preview",
		Parameters:     map[string]any{"mode": "safe"},
		Source:         Source{Type: SourceTypeManual},
		IdempotencyKey: &idempotencyKey,
	}
	digest, err := requestDigest(request, request.Parameters, nil)
	if err != nil {
		t.Fatalf("requestDigest() error = %v", err)
	}
	want := Run{
		ID:            uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1209"),
		RequestDigest: digest,
	}
	runs.existing = want
	runs.getError = nil
	service.repositories = &recordingRepositories{err: errors.New("must not be called")}
	service.catalogs = &recordingCatalogs{err: errors.New("must not be called")}

	created, err := service.CreateRun(context.Background(), request)
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.ID != want.ID {
		t.Errorf("created ID = %s, want %s", created.ID, want.ID)
	}
	if runs.calls != 0 {
		t.Errorf("creation calls = %d, want 0", runs.calls)
	}
}

func TestCreateRunPinsArtifactInputToProducerRevision(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	revision := testRevision()
	target := &revision.Catalog.Targets[0]
	preview := target.Spec.Actions["preview"]
	preview.Artifacts = map[string]catalog.Artifact{
		"plan": {Path: "/artifacts/plan.tfplan"},
	}
	target.Spec.Actions["preview"] = preview
	target.Spec.Actions["apply"] = catalog.Action{
		Command: []string{"terraform", "apply"},
		Inputs: map[string]catalog.Input{
			"plan": {
				Artifact:  catalog.ArtifactReference{FromAction: "preview"},
				MountPath: "/inputs/plan.tfplan",
			},
		},
	}
	service.catalogs = &recordingCatalogs{revision: revision}
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300")
	producerRunID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1301")
	runs.artifacts = map[uuid.UUID]ArtifactInput{
		artifactID: {
			ArtifactID: artifactID, ProducerRunID: producerRunID, Name: "plan",
			MediaType: "application/octet-stream", SizeBytes: 12,
			SHA256: strings.Repeat("a", 64), RetentionState: "retained",
			RepositoryID: testRepositoryID, CatalogRevisionID: testRevisionID,
			CommitSHA: testCommitSHA, TargetName: "network", ActionName: "preview",
		},
	}

	created, err := service.CreateRun(context.Background(), CreateRequest{
		RepositoryID: testRepositoryID,
		TargetName:   "network",
		ActionName:   "apply",
		Inputs:       map[string]uuid.UUID{"plan": artifactID},
		Source:       Source{Type: SourceTypeManual},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	input := created.Snapshot.Inputs["plan"]
	if created.CatalogRevisionID != testRevisionID || created.CommitSHA != testCommitSHA ||
		input.ArtifactID != artifactID || input.ProducerRunID != producerRunID {
		t.Errorf("created input-pinned Run = %#v", created)
	}
	if runs.created.Snapshot.Inputs["plan"].SHA256 != strings.Repeat("a", 64) {
		t.Errorf("persisted input = %#v", runs.created.Snapshot.Inputs["plan"])
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
		RepositoryID:      testRepositoryID,
		TargetName:        "network",
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
		RepositoryID:      testRepositoryID,
		TargetName:        "network",
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

func TestCreateRunRejectsInvalidSourceMetadata(t *testing.T) {
	t.Parallel()

	deliveryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1304")
	revisionID := testRevisionID
	deliveryKey := deliveryID.String()
	base := CreateRequest{
		RepositoryID:      testRepositoryID,
		TargetName:        "network",
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
			want: "manual and API Runs cannot pin",
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

func TestCreateRunRejectsIncompatibleArtifactInput(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	revision := testRevision()
	revision.Catalog.Targets[0].Spec.Actions["apply"] = catalog.Action{
		Command: []string{"terraform", "apply"},
		Inputs: map[string]catalog.Input{
			"plan": {
				Artifact:  catalog.ArtifactReference{FromAction: "preview"},
				MountPath: "/inputs/plan.tfplan",
			},
		},
	}
	service.catalogs = &recordingCatalogs{revision: revision}
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300")
	runs.artifacts = map[uuid.UUID]ArtifactInput{
		artifactID: {
			ArtifactID: artifactID, ProducerRunID: uuid.New(), Name: "plan",
			SHA256: strings.Repeat("a", 64), RetentionState: "retained",
			RepositoryID: testRepositoryID, CatalogRevisionID: testRevisionID,
			CommitSHA: testCommitSHA, TargetName: "network", ActionName: "destroy",
		},
	}

	_, err := service.CreateRun(context.Background(), CreateRequest{
		RepositoryID: testRepositoryID,
		TargetName:   "network",
		ActionName:   "apply",
		Inputs:       map[string]uuid.UUID{"plan": artifactID},
		Source:       Source{Type: SourceTypeAPI},
	})
	if !errors.Is(err, ErrInvalidInputs) {
		t.Fatalf("CreateRun() error = %v, want invalid inputs", err)
	}
	if runs.calls != 0 {
		t.Errorf("persistence calls = %d, want 0", runs.calls)
	}
}

func TestListRunsAppliesDefaultsAndFilters(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	want := []Run{{ID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1400")}}
	runs.listed = want
	repositoryID := testRepositoryID

	listed, err := service.ListRuns(
		context.Background(),
		RunListFilter{
			RepositoryID: &repositoryID,
			TargetName:   "network",
			ClusterName:  "colo",
			Phase:        PhaseRunning,
		},
	)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != want[0].ID || listed.Total != 1 {
		t.Errorf("ListRuns() = %#v, want %#v", listed, want)
	}
	if runs.listFilter.RepositoryID == nil ||
		*runs.listFilter.RepositoryID != repositoryID ||
		runs.listFilter.TargetName != "network" ||
		runs.listFilter.ClusterName != "colo" ||
		runs.listFilter.Phase != PhaseRunning ||
		runs.listFilter.Limit != DefaultRunListLimit {
		t.Errorf("ListRuns() filter = %#v", runs.listFilter)
	}
}

func TestListRunsRejectsInvalidPhase(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{Phase: "waiting"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{Limit: MaximumRunListLimit + 1})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{Offset: -1})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsTargetWithoutRepository(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{TargetName: "network"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

const testCommitSHA = "0123456789abcdef0123456789abcdef01234567"

var (
	testRepositoryID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	testRevisionID   = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
)

func testService(t *testing.T) (*Service, *recordingRuns) {
	t.Helper()

	placements, err := placement.NewSet(map[string]placement.Config{
		"infrastructure": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-jobs",
			Credentials: map[string]placement.CredentialSource{
				"backend": {
					SecretRef: placement.SecretReference{Name: "terraform-backend"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create placements: %v", err)
	}

	runs := &recordingRuns{}
	service := NewService(Dependencies{
		Creation: runs,
		History:  runs,
		Repositories: &recordingRepositories{
			record: repository.Repository{
				Config: repository.Config{
					Name:     "infrastructure",
					CloneURL: "https://github.com/example/infrastructure.git",
					Branch:   "main",
				},
				ID: testRepositoryID,
			},
		},
		Catalogs:   &recordingCatalogs{revision: testRevision()},
		Placements: placements,
	})

	return service, runs
}

func testRevision() catalog.Revision {
	return catalog.Revision{
		ID:        testRevisionID,
		CommitSHA: testCommitSHA,
		Catalog: catalog.Catalog{
			Runtimes: []catalog.Runtime{{
				APIVersion: catalog.APIVersion,
				Kind:       catalog.RuntimeKind,
				Metadata:   catalog.Metadata{Name: "terraform"},
				Spec: catalog.RuntimeSpec{
					Image: "hashicorp/terraform:1.14.3",
				},
			}},
			Targets: []catalog.Target{{
				APIVersion: catalog.APIVersion,
				Kind:       catalog.TargetKind,
				Metadata:   catalog.Metadata{Name: "network"},
				Spec: catalog.TargetSpec{
					Runtime:   "terraform",
					Placement: "infrastructure",
					Timeout:   "30m",
					Actions: map[string]catalog.Action{
						"preview": {
							Command: []string{"terraform", "plan"},
							Parameters: map[string]catalog.Parameter{
								"mode": {
									Type:     catalog.ParameterTypeEnum,
									Required: true,
									Values:   []string{"safe", "fast"},
									Argument: &catalog.ParameterArgument{Name: "--mode"},
								},
								"refresh": {
									Type:     catalog.ParameterTypeBoolean,
									Argument: &catalog.ParameterArgument{Name: "--refresh"},
								},
								"retries": {
									Type:     catalog.ParameterTypeNumber,
									Argument: &catalog.ParameterArgument{Name: "--retries"},
								},
								"message": {
									Type:     catalog.ParameterTypeString,
									Argument: &catalog.ParameterArgument{Name: "--message"},
								},
							},
						},
					},
				},
			}},
		},
	}
}

type recordingRuns struct {
	calls                  int
	created                Run
	err                    error
	existing               Run
	getError               error
	idempotencyPrincipalID *uuid.UUID
	artifacts              map[uuid.UUID]ArtifactInput
	listFilter             RunListFilter
	listed                 []Run
	listError              error
	loaded                 Run
	historyError           error
}

func (s *recordingRuns) GetRun(context.Context, uuid.UUID) (Run, error) {
	if s.historyError != nil {
		return Run{}, s.historyError
	}
	if s.loaded.ID == uuid.Nil {
		return Run{}, ErrRunNotFound
	}

	return s.loaded, nil
}

func (s *recordingRuns) ListRuns(_ context.Context, filter RunListFilter) (RunList, error) {
	s.listFilter = filter

	return RunList{Items: s.listed, Total: int64(len(s.listed))}, s.listError
}

func (s *recordingRuns) GetArtifactInputCandidate(_ context.Context, artifactID uuid.UUID) (ArtifactInput, error) {
	input, found := s.artifacts[artifactID]
	if !found {
		return ArtifactInput{}, ErrArtifactNotFound
	}

	return input, nil
}

func (s *recordingRuns) CreateRun(_ context.Context, record Run) (Run, error) {
	s.calls++
	s.created = record

	return record, s.err
}

func (s *recordingRuns) GetRunByIdempotencyKey(
	_ context.Context,
	principalID *uuid.UUID,
	_ SourceType,
	_ string,
) (Run, error) {
	if principalID != nil {
		cloned := *principalID
		s.idempotencyPrincipalID = &cloned
	}
	if s.getError == nil && s.existing.ID != uuid.Nil {
		return s.existing, nil
	}
	if s.getError != nil {
		return Run{}, s.getError
	}

	return Run{}, ErrRunNotFound
}

type recordingRepositories struct {
	record repository.Repository
	err    error
}

func (s *recordingRepositories) GetRepository(context.Context, uuid.UUID) (repository.Repository, error) {
	return s.record, s.err
}

type recordingCatalogs struct {
	revision catalog.Revision
	err      error
}

type pinnedCatalogs struct {
	active      catalog.Revision
	pinned      catalog.Revision
	activeCalls int
	pinnedCalls int
}

func (s *pinnedCatalogs) GetActiveRevision(context.Context, uuid.UUID) (catalog.Revision, error) {
	s.activeCalls++

	return s.active, nil
}

func (s *pinnedCatalogs) GetRevision(_ context.Context, _ uuid.UUID, revisionID uuid.UUID) (catalog.Revision, error) {
	s.pinnedCalls++
	if revisionID != s.pinned.ID {
		return catalog.Revision{}, catalog.ErrNotFound
	}

	return s.pinned, nil
}

func (s *recordingCatalogs) GetActiveRevision(context.Context, uuid.UUID) (catalog.Revision, error) {
	return s.revision, s.err
}

func (s *recordingCatalogs) GetRevision(context.Context, uuid.UUID, uuid.UUID) (catalog.Revision, error) {
	return s.revision, s.err
}

func contains(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}
