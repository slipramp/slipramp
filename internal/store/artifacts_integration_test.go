package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestArtifactPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	runID := createArtifactTestRun(t, ctx, database, createdAt)
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1403")
	record := artifact.Artifact{
		ID:             artifactID,
		RunID:          runID,
		Name:           "log",
		Kind:           artifact.KindLog,
		State:          artifact.StateOpen,
		MediaType:      "text/plain; charset=utf-8",
		RetentionState: artifact.RetentionRetained,
		CreatedAt:      createdAt.Add(time.Minute),
	}

	created, err := database.CreateArtifact(ctx, record)
	if err != nil {
		t.Fatalf("CreateArtifact() error = %v", err)
	}
	if created != record {
		t.Errorf("CreateArtifact() = %#v, want %#v", created, record)
	}

	loaded, err := database.GetArtifact(ctx, artifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	if loaded != record {
		t.Errorf("GetArtifact() = %#v, want %#v", loaded, record)
	}

	loaded, err = database.GetArtifactByRunAndName(ctx, runID, "log")
	if err != nil {
		t.Fatalf("GetArtifactByRunAndName() error = %v", err)
	}
	if loaded.ID != artifactID {
		t.Errorf("GetArtifactByRunAndName() ID = %s, want %s", loaded.ID, artifactID)
	}

	duplicate := record
	duplicate.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1404")
	if _, err := database.CreateArtifact(ctx, duplicate); !errors.Is(
		err,
		artifact.ErrAlreadyExists,
	) {
		t.Errorf("duplicate CreateArtifact() error = %v, want already exists", err)
	}

	objectKey := "runs/018f90ff/artifacts/plan"
	output := artifact.Artifact{
		ID:             uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1405"),
		RunID:          runID,
		Name:           "plan",
		Kind:           artifact.KindOutput,
		State:          artifact.StateOpen,
		MediaType:      "application/octet-stream",
		Sensitive:      true,
		ObjectKey:      &objectKey,
		RetentionState: artifact.RetentionRetained,
		CreatedAt:      createdAt.Add(2 * time.Minute),
	}
	if _, err := database.CreateArtifact(ctx, output); err != nil {
		t.Fatalf("create output Artifact: %v", err)
	}

	artifacts, err := database.ListArtifactsForRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListArtifactsForRun() error = %v", err)
	}
	if len(artifacts) != 2 || artifacts[0].ID != artifactID ||
		artifacts[1].ID != output.ID {
		t.Errorf("ListArtifactsForRun() = %#v", artifacts)
	}

	later := artifact.LogChunk{
		ArtifactID:  artifactID,
		OffsetBytes: 5,
		SizeBytes:   6,
		SHA256:      strings.Repeat("b", 64),
		ObjectKey:   "runs/018f90ff/artifacts/log/chunks/5-b",
		CreatedAt:   createdAt.Add(4 * time.Minute),
	}
	first := artifact.LogChunk{
		ArtifactID:  artifactID,
		OffsetBytes: 0,
		SizeBytes:   5,
		SHA256:      strings.Repeat("a", 64),
		ObjectKey:   "runs/018f90ff/artifacts/log/chunks/0-a",
		CreatedAt:   createdAt.Add(3 * time.Minute),
	}
	for _, chunk := range []artifact.LogChunk{later, first} {
		createdChunk, err := database.CreateLogChunk(ctx, chunk)
		if err != nil {
			t.Fatalf("CreateLogChunk(%d) error = %v", chunk.OffsetBytes, err)
		}
		if createdChunk != chunk {
			t.Errorf("CreateLogChunk(%d) = %#v, want %#v", chunk.OffsetBytes, createdChunk, chunk)
		}
	}

	if _, err := database.CreateLogChunk(ctx, first); !errors.Is(
		err,
		artifact.ErrAlreadyExists,
	) {
		t.Errorf("duplicate CreateLogChunk() error = %v, want already exists", err)
	}
	wrongArtifact := first
	wrongArtifact.ArtifactID = output.ID
	wrongArtifact.ObjectKey = "runs/018f90ff/artifacts/plan/chunks/0-a"
	if _, err := database.CreateLogChunk(ctx, wrongArtifact); !errors.Is(
		err,
		artifact.ErrNotFound,
	) {
		t.Errorf("output CreateLogChunk() error = %v, want not found", err)
	}

	loadedChunk, err := database.GetLogChunk(ctx, artifactID, first.OffsetBytes)
	if err != nil {
		t.Fatalf("GetLogChunk() error = %v", err)
	}
	if loadedChunk != first {
		t.Errorf("GetLogChunk() = %#v, want %#v", loadedChunk, first)
	}

	chunks, err := database.ListLogChunks(ctx, artifactID, 0)
	if err != nil {
		t.Fatalf("ListLogChunks() error = %v", err)
	}
	if len(chunks) != 2 || chunks[0] != first || chunks[1] != later {
		t.Errorf("ListLogChunks() = %#v", chunks)
	}

	chunks, err = database.ListLogChunks(ctx, artifactID, later.OffsetBytes)
	if err != nil {
		t.Fatalf("ListLogChunks(from later) error = %v", err)
	}
	if len(chunks) != 1 || chunks[0] != later {
		t.Errorf("ListLogChunks(from later) = %#v", chunks)
	}
}

func createArtifactTestRun(
	t *testing.T,
	ctx context.Context,
	database *Database,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1400")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1401")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1402")
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:        repositoryID,
		Name:      "artifact-test",
		CloneURL:  "https://github.com/example/artifact-test.git",
		Branch:    "main",
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}

	refresh := successfulCatalogRefresh(
		"0123456789abcdef0123456789abcdef01234567",
		createdAt.Add(time.Second),
		revisionID,
	)
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("record catalog refresh: %v", err)
	}

	_, err = database.CreateRun(ctx, run.Run{
		ID:                runID,
		Phase:             run.PhaseQueued,
		RepositoryID:      repositoryID,
		CatalogRevisionID: revisionID,
		CommitSHA:         *refresh.CommitSHA,
		TargetName:        "network",
		ActionName:        "preview",
		Parameters:        map[string]any{},
		PlacementName:     "infrastructure",
		ClusterName:       "colo",
		Source:            run.Source{Type: run.SourceTypeManual},
		RequestDigest:     strings.Repeat("c", 64),
		Snapshot: run.Snapshot{
			Repository: run.RepositorySnapshot{
				Name:     "artifact-test",
				CloneURL: "https://github.com/example/artifact-test.git",
				Branch:   "main",
			},
			Target:  refresh.Revision.Catalog.Targets[0],
			Action:  catalog.EffectiveAction{Name: "preview"},
			Runtime: refresh.Revision.Catalog.Runtimes[0],
			Placement: run.PlacementSnapshot{
				Name:         "infrastructure",
				ClusterName:  "colo",
				JobNamespace: "slipramp-jobs",
			},
		},
		CreatedAt: createdAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("create Run: %v", err)
	}

	return runID
}
