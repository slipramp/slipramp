package artifact

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/objectstore"
)

type serviceMetadataStore struct {
	runCluster string
	record     *Artifact
	chunks     map[int64]LogChunk
	events     *[]string
	context    RunContext
}

func (s *serviceMetadataStore) GetRunClusterName(_ context.Context, _ uuid.UUID) (string, error) {
	if s.runCluster == "" {
		return "", ErrNotFound
	}

	return s.runCluster, nil
}

func (s *serviceMetadataStore) CreateArtifact(_ context.Context, record Artifact) (Artifact, error) {
	if s.record != nil {
		return Artifact{}, ErrAlreadyExists
	}

	copy := record
	s.record = &copy

	return record, nil
}

func (s *serviceMetadataStore) GetArtifact(_ context.Context, _ uuid.UUID) (Artifact, error) {
	if s.record == nil {
		return Artifact{}, ErrNotFound
	}

	return *s.record, nil
}

func (s *serviceMetadataStore) GetArtifactByRunAndName(context.Context, uuid.UUID, string) (Artifact, error) {
	if s.record == nil {
		return Artifact{}, ErrNotFound
	}

	return *s.record, nil
}

func (s *serviceMetadataStore) ListArtifactsForRun(context.Context, uuid.UUID) ([]Artifact, error) {
	if s.record == nil {
		return nil, nil
	}

	return []Artifact{*s.record}, nil
}

func (s *serviceMetadataStore) GetArtifactProvenance(context.Context, uuid.UUID) (Provenance, error) {
	return Provenance{}, nil
}

func (s *serviceMetadataStore) GetRunArtifactContext(context.Context, uuid.UUID) (RunContext, error) {
	if s.runCluster == "" {
		return RunContext{}, ErrNotFound
	}

	return s.context, nil
}

func (s *serviceMetadataStore) GetRunInputArtifact(context.Context, uuid.UUID, string) (Artifact, error) {
	return Artifact{}, ErrNotFound
}

func (s *serviceMetadataStore) CreateLogChunk(_ context.Context, chunk LogChunk) (LogChunk, error) {
	if s.events != nil {
		*s.events = append(*s.events, "metadata")
	}
	if _, ok := s.chunks[chunk.OffsetBytes]; ok {
		return LogChunk{}, ErrAlreadyExists
	}

	s.chunks[chunk.OffsetBytes] = chunk
	if s.record != nil {
		s.record.SizeBytes = chunk.EndOffset()
	}

	return chunk, nil
}

func (s *serviceMetadataStore) GetLogChunk(_ context.Context, _ uuid.UUID, offsetBytes int64) (LogChunk, error) {
	chunk, ok := s.chunks[offsetBytes]
	if !ok {
		return LogChunk{}, ErrNotFound
	}

	return chunk, nil
}

func (s *serviceMetadataStore) ListLogChunks(_ context.Context, _ uuid.UUID, fromOffset int64) ([]LogChunk, error) {
	result := make([]LogChunk, 0, len(s.chunks))
	for _, chunk := range s.chunks {
		if chunk.EndOffset() > fromOffset {
			result = append(result, chunk)
		}
	}
	slices.SortFunc(result, func(left, right LogChunk) int { return int(left.OffsetBytes - right.OffsetBytes) })

	return result, nil
}

func (s *serviceMetadataStore) SealArtifact(
	_ context.Context,
	_ uuid.UUID,
	sizeBytes int64,
	digest string,
	sealedAt time.Time,
) (Artifact, error) {
	if s.record == nil {
		return Artifact{}, ErrNotFound
	}
	s.record.State = StateSealed
	s.record.SizeBytes = sizeBytes
	s.record.SHA256 = &digest
	s.record.SealedAt = &sealedAt

	return *s.record, nil
}

type serviceContentStore struct {
	puts      int
	key       string
	contents  string
	mediaType string
	err       error
	events    *[]string
	objects   map[string]string
}

func (s *serviceContentStore) Put(
	_ context.Context,
	key string,
	body io.Reader,
	size int64,
	options objectstore.PutOptions,
) (int64, error) {
	s.puts++
	s.key = key
	s.mediaType = options.MediaType
	if s.events != nil {
		*s.events = append(*s.events, "contents")
	}
	if s.err != nil {
		return 0, s.err
	}

	contents, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}
	s.contents = string(contents)
	if s.objects == nil {
		s.objects = map[string]string{}
	}
	s.objects[key] = s.contents

	return size, nil
}

func (s *serviceContentStore) Open(_ context.Context, key string) (objectstore.Object, error) {
	contents := s.contents
	if s.objects != nil {
		contents = s.objects[key]
	}
	return objectstore.Object{
		Body: io.NopCloser(strings.NewReader(contents)),
		Size: int64(len(contents)),
	}, nil
}

func TestServiceAppendsLogChunkDurably(t *testing.T) {
	t.Parallel()

	events := []string{}
	metadata := &serviceMetadataStore{
		runCluster: "colo",
		chunks:     map[int64]LogChunk{},
		events:     &events,
	}
	contents := &serviceContentStore{events: &events}
	service := NewService(metadata, contents)
	service.newID = func() uuid.UUID {
		return uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1500")
	}
	service.now = func() time.Time {
		return time.Date(2026, time.October, 17, 13, 0, 0, 0, time.UTC)
	}
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501")

	err := service.AppendLogChunk(context.Background(), AppendLogChunkRequest{
		RunID:       runID,
		ClusterName: "colo",
		OffsetBytes: 0,
		Contents:    []byte("hello logs\n"),
	})
	if err != nil {
		t.Fatalf("AppendLogChunk() error = %v", err)
	}

	if metadata.record == nil || metadata.record.Name != "log" ||
		metadata.record.Kind != KindLog || metadata.record.State != StateOpen {
		t.Errorf("log Artifact = %#v", metadata.record)
	}
	chunk := metadata.chunks[0]
	if chunk.SizeBytes != 11 ||
		chunk.SHA256 != "ec07a7212eb62510e184d9312aadc99e0d8cfaac439de3ea65f8e19eb7a74661" {
		t.Errorf("log chunk = %#v", chunk)
	}
	if contents.puts != 1 || contents.contents != "hello logs\n" ||
		contents.mediaType != "application/octet-stream" ||
		!strings.HasPrefix(contents.key, "runs/"+runID.String()+"/artifacts/log/chunks/0-") {
		t.Errorf("stored contents = %#v", contents)
	}
	if len(events) != 2 || events[0] != "contents" || events[1] != "metadata" {
		t.Errorf("persistence order = %v, want [contents metadata]", events)
	}
}

func TestServiceTreatsRepeatedLogChunkAsIdempotent(t *testing.T) {
	t.Parallel()

	metadata := &serviceMetadataStore{
		runCluster: "colo",
		chunks:     map[int64]LogChunk{},
	}
	contents := &serviceContentStore{}
	service := NewService(metadata, contents)
	request := AppendLogChunkRequest{
		RunID:       uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501"),
		ClusterName: "colo",
		OffsetBytes: 12,
		Contents:    []byte("hello logs\n"),
	}

	if err := service.AppendLogChunk(context.Background(), request); err != nil {
		t.Fatalf("first AppendLogChunk() error = %v", err)
	}
	if err := service.AppendLogChunk(context.Background(), request); err != nil {
		t.Fatalf("repeated AppendLogChunk() error = %v", err)
	}
	if contents.puts != 1 {
		t.Errorf("content puts = %d, want 1", contents.puts)
	}

	request.Contents = []byte("different\n")
	if err := service.AppendLogChunk(context.Background(), request); !errors.Is(err, ErrChunkConflict) {
		t.Errorf("conflicting AppendLogChunk() error = %v, want conflict", err)
	}
	if contents.puts != 1 {
		t.Errorf("content puts after conflict = %d, want 1", contents.puts)
	}
}

func TestServiceRejectsLogChunkFromWrongCluster(t *testing.T) {
	t.Parallel()

	metadata := &serviceMetadataStore{
		runCluster: "edge",
		chunks:     map[int64]LogChunk{},
	}
	contents := &serviceContentStore{}
	service := NewService(metadata, contents)

	err := service.AppendLogChunk(context.Background(), AppendLogChunkRequest{
		RunID:       uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501"),
		ClusterName: "colo",
		Contents:    []byte("hello logs\n"),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("AppendLogChunk() error = %v, want not found", err)
	}
	if metadata.record != nil || contents.puts != 0 {
		t.Error("wrong-cluster request persisted data")
	}
}

func TestServiceRejectsOversizedLogChunk(t *testing.T) {
	t.Parallel()

	service := NewService(&serviceMetadataStore{chunks: map[int64]LogChunk{}}, &serviceContentStore{})
	err := service.AppendLogChunk(context.Background(), AppendLogChunkRequest{
		RunID:       uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501"),
		ClusterName: "colo",
		Contents:    make([]byte, MaxLogChunkSizeBytes+1),
	})
	if !errors.Is(err, ErrChunkTooLarge) {
		t.Fatalf("AppendLogChunk() error = %v, want too large", err)
	}
}

func TestServiceSealsAndReconnectsToDurableLog(t *testing.T) {
	t.Parallel()

	metadata := &serviceMetadataStore{
		runCluster: "colo",
		chunks:     map[int64]LogChunk{},
	}
	contents := &serviceContentStore{}
	service := NewService(metadata, contents)
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501")

	for _, request := range []AppendLogChunkRequest{
		{RunID: runID, ClusterName: "colo", Contents: []byte("hello ")},
		{RunID: runID, ClusterName: "colo", OffsetBytes: 6, Contents: []byte("logs\n")},
	} {
		if err := service.AppendLogChunk(context.Background(), request); err != nil {
			t.Fatalf("AppendLogChunk() error = %v", err)
		}
	}

	if err := service.SealLog(context.Background(), SealLogRequest{
		RunID:       runID,
		ClusterName: "colo",
		SizeBytes:   11,
		SHA256:      "ec07a7212eb62510e184d9312aadc99e0d8cfaac439de3ea65f8e19eb7a74661",
	}); err != nil {
		t.Fatalf("SealLog() error = %v", err)
	}
	if err := service.SealLog(context.Background(), SealLogRequest{
		RunID:       runID,
		ClusterName: "colo",
		SizeBytes:   11,
		SHA256:      "ec07a7212eb62510e184d9312aadc99e0d8cfaac439de3ea65f8e19eb7a74661",
	}); err != nil {
		t.Fatalf("repeated SealLog() error = %v", err)
	}

	stream, err := service.StreamLog(context.Background(), runID, 6)
	if err != nil {
		t.Fatalf("StreamLog() error = %v", err)
	}
	events, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if !strings.Contains(string(events), "bG9ncwo=") ||
		!strings.Contains(string(events), "event: sealed") {
		t.Errorf("events = %q", events)
	}
}

func TestServiceUploadsDeclaredArtifactIdempotently(t *testing.T) {
	t.Parallel()

	metadata := &serviceMetadataStore{
		runCluster: "colo",
		chunks:     map[int64]LogChunk{},
		context: RunContext{Artifacts: map[string]Declaration{
			"plan": {MediaType: "application/octet-stream", Sensitive: true},
		}},
	}
	contents := &serviceContentStore{}
	service := NewService(metadata, contents)
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501")
	digest := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

	request := UploadRequest{
		RunID: runID, Name: "plan", Body: strings.NewReader("abc"), SizeBytes: 3, SHA256: digest,
	}
	if err := service.Upload(context.Background(), request); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	request.Body = strings.NewReader("ignored because metadata already exists")
	if err := service.Upload(context.Background(), request); err != nil {
		t.Fatalf("repeated Upload() error = %v", err)
	}
	if contents.puts != 1 {
		t.Errorf("content puts = %d, want 1", contents.puts)
	}
	if metadata.record == nil || !metadata.record.Sensitive || metadata.record.SHA256 == nil ||
		*metadata.record.SHA256 != digest {
		t.Errorf("artifact = %#v", metadata.record)
	}

	request.SizeBytes = 4
	if err := service.Upload(context.Background(), request); !errors.Is(err, ErrChunkConflict) {
		t.Errorf("conflicting Upload() error = %v", err)
	}
}
