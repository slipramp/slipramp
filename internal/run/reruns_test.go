package run

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

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
		TargetID:          testTargetID,
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
