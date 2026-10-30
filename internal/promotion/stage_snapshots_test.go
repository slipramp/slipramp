package promotion

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

type recordingStageSnapshotStore struct {
	snapshot StageSnapshot
	stages   []Stage
	err      error
}

func (s *recordingStageSnapshotStore) ReplacePromotionStageSnapshot(
	_ context.Context,
	snapshot StageSnapshot,
	stages []Stage,
) error {
	s.snapshot = snapshot
	s.stages = stages

	return s.err
}

type staticRepositoryLister []repository.Repository

func (s staticRepositoryLister) ListRepositories(context.Context) ([]repository.Repository, error) {
	return s, nil
}

func TestStageServiceReplacesSnapshotWithMatchedRepository(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 1, 0, time.UTC)
	store := &recordingStageSnapshotStore{}
	service := NewStageService(
		IntegrationSet{
			"prod": {
				ClusterName:  "colo",
				Repositories: []string{"ops"},
			},
		},
		staticRepositoryLister{
			{
				ID:       uuid.New(),
				Name:     "ops",
				CloneURL: "https://example.com/ops.git",
				Branch:   "main",
			},
		},
		store,
	)
	service.now = func() time.Time { return now }
	stageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1901")
	healthStatus := "Healthy"
	argoNamespace := "argocd"
	service.newID = func() uuid.UUID { return stageID }

	err := service.ReplaceStageSnapshot(t.Context(), "prod", "colo", StageSnapshotReplacement{
		SnapshotID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1900"),
		ObservedAt: now.Add(-time.Second),
		Stages: []StageObservation{
			{
				Project:                  "kargo-core",
				Name:                     "external-dns-prod",
				KubernetesUID:            "stage-uid",
				ResourceVersion:          "42",
				Application:              "external-dns",
				Environment:              "prod",
				RepositoryURL:            "https://example.com/ops.git",
				TargetBranch:             "stage/external-dns/prod",
				SourcePaths:              []string{"k8s/external-dns/base"},
				Freight:                  Freight{Items: []FreightItem{}},
				DirectFreight:            Freight{Items: []FreightItem{{Origin: "Warehouse/app", Name: "freight-new"}}},
				AutoPromotionEnabled:     true,
				HealthStatus:             &healthStatus,
				ArgoApplicationNamespace: &argoNamespace,
			},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceStageSnapshot() error = %v", err)
	}
	if store.snapshot.ClusterName != "colo" || len(store.stages) != 1 {
		t.Fatalf("stored snapshot = %#v, stages = %#v", store.snapshot, store.stages)
	}
	healthStatus = "Changed"
	argoNamespace = "changed"
	stage := store.stages[0]
	if stage.ID != stageID || stage.RepositoryName != "ops" || stage.Integration != "prod" ||
		stage.ClusterName != "colo" || !stage.AutoPromotionEnabled ||
		stage.HealthStatus == nil || *stage.HealthStatus != "Healthy" ||
		stage.ArgoApplicationNamespace == nil || *stage.ArgoApplicationNamespace != "argocd" ||
		len(stage.DirectFreight.Items) != 1 || stage.DirectFreight.Items[0].Name != "freight-new" {
		t.Errorf("stored Stage = %#v", stage)
	}
	stage.RequestedFreight = []FreightRequest{{Origin: "Warehouse/app", SourceStages: []string{"app-qa"}}}
	stage.Freight = Freight{Items: []FreightItem{{
		Name: "freight-42", Artifacts: []FreightArtifact{{Repository: new("ghcr.io/example/app"), Version: "1.0"}},
	}}}
	stage.SuccessfulRenderAcknowledgement = &SuccessfulRenderAcknowledgement{SourceRevision: "observed"}
	frozen := stage.PromotionSnapshot()
	stage.SourcePaths[0] = "changed"
	stage.RequestedFreight[0].SourceStages[0] = "changed"
	*stage.Freight.Items[0].Artifacts[0].Repository = "changed"
	stage.SuccessfulRenderAcknowledgement.SourceRevision = "changed"
	if frozen.SourcePaths[0] != "k8s/external-dns/base" || frozen.RequestedFreight[0].SourceStages[0] != "app-qa" ||
		*frozen.Freight.Items[0].Artifacts[0].Repository != "ghcr.io/example/app" ||
		frozen.SuccessfulRenderAcknowledgement.SourceRevision != "observed" {
		t.Errorf("Promotion snapshot changed with observed Stage = %#v", frozen)
	}
}

func TestStageServiceRejectsWrongClusterAndUnmatchedRepository(t *testing.T) {
	t.Parallel()

	service := NewStageService(
		IntegrationSet{
			"prod": {
				ClusterName:  "colo",
				Repositories: []string{"ops"},
			},
		},
		staticRepositoryLister{},
		&recordingStageSnapshotStore{},
	)

	err := service.ReplaceStageSnapshot(t.Context(), "prod", "edge", StageSnapshotReplacement{})
	if !errors.Is(err, ErrIntegrationCluster) {
		t.Fatalf("wrong-cluster error = %v", err)
	}

	service.now = func() time.Time { return time.Date(2026, time.October, 21, 12, 0, 1, 0, time.UTC) }
	err = service.ReplaceStageSnapshot(t.Context(), "prod", "colo", StageSnapshotReplacement{
		SnapshotID: uuid.New(),
		ObservedAt: service.now().Add(-time.Second),
		Stages: []StageObservation{
			{
				Project:         "kargo-core",
				Name:            "external-dns-prod",
				KubernetesUID:   "stage-uid",
				ResourceVersion: "42",
				Application:     "external-dns",
				Environment:     "prod",
				RepositoryURL:   "https://example.com/unconfigured.git",
				TargetBranch:    "stage/external-dns/prod",
				SourcePaths:     []string{"k8s/external-dns/base"},
				Freight:         Freight{Items: []FreightItem{}},
			},
		},
	})
	if !errors.Is(err, ErrRepositoryNotAllowed) {
		t.Fatalf("repository error = %v", err)
	}
}

func TestStageServiceCapsFutureObservationTimeAtAcceptance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 1, 0, time.UTC)
	store := &recordingStageSnapshotStore{}
	service := NewStageService(
		IntegrationSet{
			"prod": {
				ClusterName:  "colo",
				Repositories: []string{"ops"},
			},
		},
		staticRepositoryLister{},
		store,
	)
	service.now = func() time.Time { return now }

	err := service.ReplaceStageSnapshot(t.Context(), "prod", "colo", StageSnapshotReplacement{
		SnapshotID: uuid.New(),
		ObservedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ReplaceStageSnapshot() error = %v", err)
	}
	if !store.snapshot.ObservedAt.Equal(now) || !store.snapshot.AcceptedAt.Equal(now) {
		t.Fatalf("stored snapshot = %#v, want observation and acceptance at %v", store.snapshot, now)
	}
}
