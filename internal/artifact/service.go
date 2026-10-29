package artifact

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/objectstore"
	"github.com/slipramp/slipramp/internal/runreport"
)

type ContentStore interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, options objectstore.PutOptions) (int64, error)
	Open(ctx context.Context, key string) (objectstore.Object, error)
}

type RunStore interface {
	GetRunClusterName(ctx context.Context, runID uuid.UUID) (string, error)
	GetArtifactProvenance(ctx context.Context, runID uuid.UUID) (Provenance, error)
	GetRunArtifactContext(ctx context.Context, runID uuid.UUID) (RunContext, error)
	GetRunInputArtifact(ctx context.Context, runID uuid.UUID, inputName string) (Artifact, error)
}

type ArtifactStore interface {
	CreateArtifact(ctx context.Context, record Artifact) (Artifact, error)
	CreateArtifactWithReport(
		ctx context.Context,
		record Artifact,
		report runreport.ArtifactReport,
	) (Artifact, error)
	GetArtifact(ctx context.Context, artifactID uuid.UUID) (Artifact, error)
	GetArtifactByRunAndName(ctx context.Context, runID uuid.UUID, name string) (Artifact, error)
	GetArtifactStorageUsage(ctx context.Context) (StorageUsage, error)
	GetArtifactReport(ctx context.Context, artifactID uuid.UUID) (runreport.ArtifactReport, error)
	ListArtifactsForRun(ctx context.Context, runID uuid.UUID) ([]Artifact, error)
	ListArtifactReportsForRun(ctx context.Context, runID uuid.UUID) ([]runreport.ArtifactReport, error)
	FailArtifact(ctx context.Context, artifactID uuid.UUID) (Artifact, error)
	SealArtifact(
		ctx context.Context,
		artifactID uuid.UUID,
		sizeBytes int64,
		sha256 string,
		sealedAt time.Time,
	) (Artifact, error)
}

type LogChunkStore interface {
	CreateLogChunk(ctx context.Context, chunk LogChunk) (LogChunk, error)
	GetLogChunk(ctx context.Context, artifactID uuid.UUID, offsetBytes int64) (LogChunk, error)
	ListLogChunks(ctx context.Context, artifactID uuid.UUID, fromOffsetBytes int64) ([]LogChunk, error)
}

type Service struct {
	runs           RunStore
	artifacts      ArtifactStore
	logChunks      LogChunkStore
	contents       ContentStore
	reportObserver ReportIngestionObserver
	newID          func() uuid.UUID
	now            func() time.Time
	tokenKey       []byte
}

type Dependencies struct {
	Runs           RunStore
	Artifacts      ArtifactStore
	LogChunks      LogChunkStore
	Contents       ContentStore
	ReportObserver ReportIngestionObserver
	TokenKey       []byte
}

func NewService(dependencies Dependencies) *Service {
	tokenKey := []byte("slipramp-artifact-test-token-key")
	if len(dependencies.TokenKey) > 0 {
		tokenKey = bytes.Clone(dependencies.TokenKey)
	}

	return &Service{
		runs:           dependencies.Runs,
		artifacts:      dependencies.Artifacts,
		logChunks:      dependencies.LogChunks,
		contents:       dependencies.Contents,
		reportObserver: reportIngestionObserverOrNoop(dependencies.ReportObserver),
		newID:          uuid.New,
		now:            time.Now,
		tokenKey:       tokenKey,
	}
}

func (s *Service) RunToken(runID uuid.UUID) string {
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write([]byte(runID.String()))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) AuthenticateRunToken(runID uuid.UUID, token string) bool {
	want := s.RunToken(runID)

	return hmac.Equal([]byte(want), []byte(token))
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
