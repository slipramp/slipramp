package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/objectstore"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestArtifactTransferIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT is not set")
	}

	objectConfig := objectstore.DefaultConfig()
	objectConfig.Endpoint = endpoint
	objectConfig.Region = os.Getenv("TEST_S3_REGION")
	objectConfig.Bucket = os.Getenv("TEST_S3_BUCKET")
	objectConfig.AccessKeyID = os.Getenv("TEST_S3_ACCESS_KEY_ID")
	objectConfig.SecretAccessKey = os.Getenv("TEST_S3_SECRET_ACCESS_KEY")
	objects, err := objectstore.New(objectConfig)
	if err != nil {
		t.Fatalf("create object store: %v", err)
	}
	if err := objects.Check(ctx); err != nil {
		t.Fatalf("check object store: %v", err)
	}

	createdAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	runID := createArtifactTestRun(t, ctx, database, createdAt)
	service := artifact.NewService(
		database,
		objects,
		[]byte("integration-artifact-token-key-32-bytes"),
	)

	contents := []byte("durable plan contents")
	digest := sha256.Sum256(contents)
	encodedDigest := hex.EncodeToString(digest[:])
	upload := artifact.UploadRequest{
		RunID:     runID,
		Name:      "plan",
		Body:      bytes.NewReader(contents),
		SizeBytes: int64(len(contents)),
		SHA256:    encodedDigest,
	}
	if err := service.Upload(ctx, upload); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	upload.Body = bytes.NewReader(contents)
	if err := service.Upload(ctx, upload); err != nil {
		t.Fatalf("idempotent Upload() error = %v", err)
	}

	records, err := service.List(ctx, runID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].Name != "plan" ||
		records[0].SHA256 == nil || *records[0].SHA256 != encodedDigest {
		t.Fatalf("List() = %#v", records)
	}

	content, err := service.OpenArtifact(ctx, records[0].ID)
	if err != nil {
		t.Fatalf("OpenArtifact() error = %v", err)
	}
	downloaded, readErr := io.ReadAll(content.Body)
	closeErr := content.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read artifact contents: %v", err)
	}
	if !bytes.Equal(downloaded, contents) {
		t.Fatalf("downloaded contents = %q, want %q", downloaded, contents)
	}

	conflict := upload
	conflict.Body = strings.NewReader("other")
	conflict.SizeBytes = 5
	otherDigest := sha256.Sum256([]byte("other"))
	conflict.SHA256 = hex.EncodeToString(otherDigest[:])
	if err := service.Upload(ctx, conflict); !errors.Is(err, artifact.ErrChunkConflict) {
		t.Fatalf("conflicting Upload() error = %v, want chunk conflict", err)
	}

	logContents := []byte("first line\nsecond line\n")
	if err := service.AppendLogChunk(ctx, artifact.AppendLogChunkRequest{
		RunID:       runID,
		ClusterName: "colo",
		OffsetBytes: 0,
		Contents:    logContents,
	}); err != nil {
		t.Fatalf("AppendLogChunk() error = %v", err)
	}
	logDigest := sha256.Sum256(logContents)
	encodedLogDigest := hex.EncodeToString(logDigest[:])
	if err := service.SealLog(ctx, artifact.SealLogRequest{
		RunID:       runID,
		ClusterName: "colo",
		SizeBytes:   int64(len(logContents)),
		SHA256:      encodedLogDigest,
	}); err != nil {
		t.Fatalf("SealLog() error = %v", err)
	}

	records, err = service.List(ctx, runID)
	if err != nil {
		t.Fatalf("List() after log seal error = %v", err)
	}
	var logID uuid.UUID
	for _, record := range records {
		if record.Kind == artifact.KindLog {
			logID = record.ID
		}
	}
	if logID == uuid.Nil {
		t.Fatal("sealed log was not listed")
	}
	logContent, err := service.OpenArtifact(ctx, logID)
	if err != nil {
		t.Fatalf("OpenArtifact(log) error = %v", err)
	}
	downloadedLog, readErr := io.ReadAll(logContent.Body)
	closeErr = logContent.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read log contents: %v", err)
	}
	if !bytes.Equal(downloadedLog, logContents) {
		t.Fatalf("downloaded log = %q, want %q", downloadedLog, logContents)
	}

	for _, record := range records {
		if record.ObjectKey != nil {
			key := *record.ObjectKey
			t.Cleanup(func() {
				if err := objects.Delete(context.Background(), key); err != nil {
					t.Errorf("delete test artifact %q: %v", key, err)
				}
			})
		}
		if record.Kind != artifact.KindLog {
			continue
		}
		chunks, err := database.ListLogChunks(ctx, record.ID, 0)
		if err != nil {
			t.Fatalf("list log chunks for cleanup: %v", err)
		}
		for _, chunk := range chunks {
			key := chunk.ObjectKey
			t.Cleanup(func() {
				if err := objects.Delete(context.Background(), key); err != nil {
					t.Errorf("delete test log chunk %q: %v", key, err)
				}
			})
		}
	}
}

func TestArtifactPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	runID := createArtifactTestRun(t, ctx, database, createdAt)
	clusterName, err := database.GetRunClusterName(ctx, runID)
	if err != nil {
		t.Fatalf("GetRunClusterName() error = %v", err)
	}
	if clusterName != "colo" {
		t.Errorf("Run cluster name = %q, want colo", clusterName)
	}

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
	outputDigest := strings.Repeat("e", 64)
	outputSealedAt := createdAt.Add(2 * time.Minute)
	output := artifact.Artifact{
		ID:             uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1405"),
		RunID:          runID,
		Name:           "plan",
		Kind:           artifact.KindOutput,
		State:          artifact.StateSealed,
		MediaType:      "application/octet-stream",
		Sensitive:      true,
		SizeBytes:      128,
		SHA256:         &outputDigest,
		ObjectKey:      &objectKey,
		RetentionState: artifact.RetentionRetained,
		CreatedAt:      outputSealedAt,
		SealedAt:       &outputSealedAt,
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

	candidate, err := database.GetArtifactInputCandidate(ctx, output.ID)
	if err != nil {
		t.Fatalf("GetArtifactInputCandidate() error = %v", err)
	}
	consumerID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1406")
	_, err = database.CreateRun(ctx, run.Run{
		ID: consumerID, Phase: run.PhaseQueued,
		RepositoryID: candidate.RepositoryID, CatalogRevisionID: candidate.CatalogRevisionID,
		CommitSHA: candidate.CommitSHA, TargetName: candidate.TargetName, ActionName: "apply",
		Parameters: map[string]any{}, PlacementName: "infrastructure", ClusterName: "colo",
		Source: run.Source{Type: run.SourceTypeAPI}, RequestDigest: strings.Repeat("f", 64),
		Snapshot: run.Snapshot{
			Inputs: map[string]run.ArtifactInput{"plan": candidate},
		},
		CreatedAt: createdAt.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create artifact-consuming Run: %v", err)
	}
	bound, err := database.GetRunInputArtifact(ctx, consumerID, "plan")
	if err != nil || bound.ID != output.ID {
		t.Fatalf("GetRunInputArtifact() = %#v, error = %v", bound, err)
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
	if _, err := database.CreateLogChunk(ctx, later); !errors.Is(
		err,
		artifact.ErrChunkConflict,
	) {
		t.Errorf("gapped CreateLogChunk() error = %v, want conflict", err)
	}
	for _, chunk := range []artifact.LogChunk{first, later} {
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

	sealedAt := createdAt.Add(5 * time.Minute)
	sealed, err := database.SealArtifact(
		ctx,
		artifactID,
		11,
		strings.Repeat("d", 64),
		sealedAt,
	)
	if err != nil {
		t.Fatalf("SealArtifact() error = %v", err)
	}
	if sealed.State != artifact.StateSealed || sealed.SizeBytes != 11 ||
		sealed.SHA256 == nil || *sealed.SHA256 != strings.Repeat("d", 64) {
		t.Errorf("sealed artifact = %#v", sealed)
	}
}

func TestArtifactRetentionPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	producerRunID := createArtifactTestRun(t, ctx, database, createdAt)
	objectKey := "runs/retention/artifacts/plan/content-a"
	digest := strings.Repeat("a", 64)
	sealedAt := createdAt.Add(time.Hour)
	record := artifact.Artifact{
		ID:             uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1600"),
		RunID:          producerRunID,
		Name:           "plan",
		Kind:           artifact.KindOutput,
		State:          artifact.StateSealed,
		MediaType:      "application/octet-stream",
		SizeBytes:      128,
		SHA256:         &digest,
		ObjectKey:      &objectKey,
		RetentionState: artifact.RetentionRetained,
		CreatedAt:      sealedAt,
		SealedAt:       &sealedAt,
	}
	if _, err := database.CreateArtifact(ctx, record); err != nil {
		t.Fatalf("create retained Artifact: %v", err)
	}
	staleObjectKey := "runs/retention/artifacts/stale/content-b"
	stale := record
	stale.ID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1602")
	stale.Name = "stale"
	stale.ObjectKey = &staleObjectKey
	if _, err := database.CreateArtifact(ctx, stale); err != nil {
		t.Fatalf("create stale Artifact candidate: %v", err)
	}
	expired, err := database.ExpireArtifacts(ctx, sealedAt.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ExpireArtifacts() with active producer error = %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("ExpireArtifacts() with active producer = %#v, want none", expired)
	}
	if _, err := database.CancelRun(ctx, producerRunID, sealedAt.Add(time.Minute)); err != nil {
		t.Fatalf("finish producing Run: %v", err)
	}

	candidate, err := database.GetArtifactInputCandidate(ctx, record.ID)
	if err != nil {
		t.Fatalf("GetArtifactInputCandidate() error = %v", err)
	}
	staleCandidate, err := database.GetArtifactInputCandidate(ctx, stale.ID)
	if err != nil {
		t.Fatalf("GetArtifactInputCandidate(stale) error = %v", err)
	}
	consumerRunID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1601")
	_, err = database.CreateRun(ctx, run.Run{
		ID: consumerRunID, Phase: run.PhaseQueued,
		RepositoryID: candidate.RepositoryID, CatalogRevisionID: candidate.CatalogRevisionID,
		CommitSHA: candidate.CommitSHA, TargetName: candidate.TargetName, ActionName: "apply",
		Parameters: map[string]any{}, PlacementName: "infrastructure", ClusterName: "colo",
		Source: run.Source{Type: run.SourceTypeAPI}, RequestDigest: strings.Repeat("f", 64),
		Snapshot: run.Snapshot{
			Inputs: map[string]run.ArtifactInput{"plan": candidate},
		},
		CreatedAt: sealedAt.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create consuming Run: %v", err)
	}

	expired, err = database.ExpireArtifacts(ctx, sealedAt.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ExpireArtifacts() with active consumer error = %v", err)
	}
	if len(expired) != 1 || expired[0].ID != stale.ID {
		t.Fatalf("ExpireArtifacts() with active consumer = %#v, want only stale candidate", expired)
	}

	_, err = database.CreateRun(ctx, run.Run{
		ID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1603"), Phase: run.PhaseQueued,
		RepositoryID: candidate.RepositoryID, CatalogRevisionID: candidate.CatalogRevisionID,
		CommitSHA: candidate.CommitSHA, TargetName: candidate.TargetName, ActionName: "apply",
		Parameters: map[string]any{}, PlacementName: "infrastructure", ClusterName: "colo",
		Source: run.Source{Type: run.SourceTypeAPI}, RequestDigest: strings.Repeat("e", 64),
		Snapshot: run.Snapshot{
			Inputs: map[string]run.ArtifactInput{"stale": staleCandidate},
		},
		CreatedAt: sealedAt.Add(3 * time.Minute),
	})
	if !errors.Is(err, run.ErrInvalidInputs) {
		t.Fatalf("CreateRun() with expired input error = %v, want invalid inputs", err)
	}

	if _, err := database.CancelRun(ctx, consumerRunID, sealedAt.Add(4*time.Minute)); err != nil {
		t.Fatalf("finish consuming Run: %v", err)
	}
	expired, err = database.ExpireArtifacts(ctx, sealedAt.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ExpireArtifacts() error = %v", err)
	}
	if len(expired) != 1 || expired[0].ID != record.ID ||
		expired[0].RetentionState != artifact.RetentionExpired {
		t.Fatalf("ExpireArtifacts() = %#v", expired)
	}

	keys, err := database.ListArtifactObjectKeys(ctx, record.ID)
	if err != nil || len(keys) != 1 || keys[0] != objectKey {
		t.Fatalf("ListArtifactObjectKeys() = %v, error = %v", keys, err)
	}
	exists, err := database.ArtifactObjectKeyExists(ctx, objectKey)
	if err != nil || !exists {
		t.Fatalf("ArtifactObjectKeyExists() = %t, error = %v", exists, err)
	}

	if err := database.MarkArtifactDeleted(ctx, record.ID); err != nil {
		t.Fatalf("MarkArtifactDeleted() error = %v", err)
	}
	exists, err = database.ArtifactObjectKeyExists(ctx, objectKey)
	if err != nil || exists {
		t.Fatalf("ArtifactObjectKeyExists() after delete = %t, error = %v", exists, err)
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
			Target: refresh.Revision.Catalog.Targets[0],
			Action: catalog.EffectiveAction{
				Name: "preview",
				Artifacts: map[string]catalog.Artifact{
					"plan": {
						Path:      "plan.tfplan",
						MediaType: "application/octet-stream",
						Sensitive: true,
					},
				},
			},
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
