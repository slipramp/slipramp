package promotion

import (
	"context"
	"encoding/json"
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
	service.newID = func() uuid.UUID { return stageID }

	err := service.ReplaceStageSnapshot(t.Context(), "prod", "colo", StageSnapshotReplacement{
		SnapshotID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1900"),
		ObservedAt: now.Add(-time.Second),
		Stages: []StageObservation{
			{
				Project:         "kargo-core",
				Name:            "external-dns-prod",
				KubernetesUID:   "stage-uid",
				ResourceVersion: "42",
				Application:     "external-dns",
				Environment:     "prod",
				RepositoryURL:   "https://example.com/ops.git",
				TargetBranch:    "stage/external-dns/prod",
				SourcePaths:     []string{"k8s/external-dns/base"},
				Freight:         json.RawMessage(`{"items":[]}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceStageSnapshot() error = %v", err)
	}
	if store.snapshot.ClusterName != "colo" || len(store.stages) != 1 {
		t.Fatalf("stored snapshot = %#v, stages = %#v", store.snapshot, store.stages)
	}
	stage := store.stages[0]
	if stage.ID != stageID || stage.RepositoryName != "ops" || stage.Integration != "prod" ||
		stage.ClusterName != "colo" {
		t.Errorf("stored Stage = %#v", stage)
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
				Freight:         json.RawMessage(`{"items":[]}`),
			},
		},
	})
	if !errors.Is(err, ErrRepositoryNotAllowed) {
		t.Fatalf("repository error = %v", err)
	}
}
