package artifact

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/objectstore"
	"github.com/slipramp/slipramp/internal/runreport"
)

type serviceMetadataStore struct {
	runCluster string
	record     *Artifact
	chunks     map[int64]LogChunk
	events     *[]string
	context    RunContext
	usage      StorageUsage
	usageErr   error
	reports    map[uuid.UUID]runreport.ArtifactReport
	reportErr  error
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

func (s *serviceMetadataStore) CreateArtifactWithReport(
	_ context.Context,
	record Artifact,
	report runreport.ArtifactReport,
) (Artifact, error) {
	if s.reportErr != nil {
		return Artifact{}, s.reportErr
	}
	if s.record != nil {
		return Artifact{}, ErrAlreadyExists
	}

	copy := record
	s.record = &copy
	if s.reports == nil {
		s.reports = map[uuid.UUID]runreport.ArtifactReport{}
	}
	s.reports[record.ID] = report

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

func (s *serviceMetadataStore) GetArtifactReport(
	_ context.Context,
	artifactID uuid.UUID,
) (runreport.ArtifactReport, error) {
	report, found := s.reports[artifactID]
	if !found {
		return runreport.ArtifactReport{}, ErrNotFound
	}

	return report, nil
}

func (s *serviceMetadataStore) ListArtifactsForRun(context.Context, uuid.UUID) ([]Artifact, error) {
	if s.record == nil {
		return nil, nil
	}

	return []Artifact{*s.record}, nil
}

func (s *serviceMetadataStore) ListArtifactReportsForRun(
	context.Context,
	uuid.UUID,
) ([]runreport.ArtifactReport, error) {
	result := make([]runreport.ArtifactReport, 0, len(s.reports))
	for _, report := range s.reports {
		result = append(result, report)
	}

	return result, nil
}

func (s *serviceMetadataStore) GetArtifactStorageUsage(context.Context) (StorageUsage, error) {
	return s.usage, s.usageErr
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

func (s *serviceMetadataStore) FailArtifact(_ context.Context, _ uuid.UUID) (Artifact, error) {
	if s.record == nil {
		return Artifact{}, ErrNotFound
	}
	if s.record.State == StateSealed {
		return Artifact{}, ErrSealConflict
	}

	s.record.State = StateFailed

	return *s.record, nil
}

type serviceContentStore struct {
	beforePut func()
	puts      int
	key       string
	contents  string
	mediaType string
	err       error
	failPut   int
	events    *[]string
	objects   map[string]string
}

func newTestService(metadata *serviceMetadataStore, contents *serviceContentStore) *Service {
	return NewService(Dependencies{
		Runs:      metadata,
		Artifacts: metadata,
		LogChunks: metadata,
		Contents:  contents,
	})
}

func TestServiceListsArtifactDescriptionsFromFrozenDeclarations(t *testing.T) {
	t.Parallel()

	metadata := &serviceMetadataStore{
		runCluster: "colo",
		context: RunContext{Artifacts: map[string]Declaration{
			"plan": {Description: "Binary execution plan."},
		}},
		record: &Artifact{Name: "plan", Kind: KindOutput},
	}
	service := newTestService(metadata, &serviceContentStore{})

	records, err := service.List(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Description != "Binary execution plan." {
		t.Errorf("List() = %#v", records)
	}
}

func TestServiceGetsStorageUsageFromMetadata(t *testing.T) {
	t.Parallel()

	want := StorageUsage{ArtifactCount: 12, RecordedBytes: 3_221_225_472}
	metadata := &serviceMetadataStore{usage: want}
	service := newTestService(metadata, &serviceContentStore{})

	got, err := service.GetStorageUsage(context.Background())
	if err != nil {
		t.Fatalf("GetStorageUsage() error = %v", err)
	}
	if got != want {
		t.Errorf("GetStorageUsage() = %#v, want %#v", got, want)
	}
}

func TestServiceWrapsStorageUsageFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("database unavailable")
	service := newTestService(&serviceMetadataStore{usageErr: want}, &serviceContentStore{})

	_, err := service.GetStorageUsage(context.Background())
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "get artifact storage usage") {
		t.Fatalf("GetStorageUsage() error = %v, want wrapped %v", err, want)
	}
}

func (s *serviceContentStore) Put(
	_ context.Context,
	key string,
	body io.Reader,
	size int64,
	options objectstore.PutOptions,
) (int64, error) {
	if s.beforePut != nil {
		s.beforePut()
	}
	s.puts++
	s.key = key
	s.mediaType = options.MediaType
	if s.events != nil {
		*s.events = append(*s.events, "contents")
	}
	if s.err != nil && (s.failPut == 0 || s.failPut == s.puts) {
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
	if s.err != nil && s.failPut == 0 {
		return objectstore.Object{}, s.err
	}
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
	service := newTestService(metadata, contents)
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
	service := newTestService(metadata, contents)
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
	service := newTestService(metadata, contents)

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

	service := newTestService(&serviceMetadataStore{chunks: map[int64]LogChunk{}}, &serviceContentStore{})
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
	service := newTestService(metadata, contents)
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
	if err := service.FailLog(context.Background(), FailLogRequest{
		RunID: runID, ClusterName: "colo",
	}); !errors.Is(err, ErrSealConflict) {
		t.Fatalf("FailLog() after seal error = %v, want conflict", err)
	}

	for _, test := range []struct {
		name      string
		offset    int64
		chunks    []LogStreamChunk
		sourceErr error
	}{
		{name: "coalesced history", chunks: []LogStreamChunk{
			{OffsetBytes: 0, NextOffsetBytes: 11, Contents: []byte("hello logs\n")},
		}},
		{name: "chunk boundary", offset: 6, chunks: []LogStreamChunk{
			{OffsetBytes: 6, NextOffsetBytes: 11, Contents: []byte("logs\n")},
		}},
		{name: "inside chunk", offset: 8, chunks: []LogStreamChunk{
			{OffsetBytes: 8, NextOffsetBytes: 11, Contents: []byte("gs\n")},
		}},
		{name: "sealed end", offset: 11},
		{name: "source EOF", offset: 6, sourceErr: io.EOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			contents.err = test.sourceErr
			stream, err := service.StreamLog(context.Background(), runID, test.offset)
			if err != nil {
				t.Fatalf("StreamLog() error = %v", err)
			}
			defer func() { _ = stream.Close() }()
			if test.sourceErr != nil {
				if _, err := stream.Next(); !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
					t.Errorf("Next() error = %v, want unexpected EOF without normal completion", err)
				}

				return
			}
			for _, want := range test.chunks {
				chunk, err := stream.Next()
				if err != nil {
					t.Fatalf("Next() error = %v", err)
				}
				if chunk.OffsetBytes != want.OffsetBytes || chunk.NextOffsetBytes != want.NextOffsetBytes ||
					!slices.Equal(chunk.Contents, want.Contents) {
					t.Errorf("chunk = %#v, want %#v", chunk, want)
				}
			}
			if _, err := stream.Next(); !errors.Is(err, io.EOF) {
				t.Errorf("drained Next() error = %v, want EOF", err)
			}
		})
	}
}

func TestServiceBoundsCoalescedHistoricalLogChunks(t *testing.T) {
	t.Parallel()

	metadata := &serviceMetadataStore{runCluster: "colo", chunks: map[int64]LogChunk{}}
	contents := &serviceContentStore{}
	service := newTestService(metadata, contents)
	runID := uuid.New()
	for offset := range int64(maximumLogStreamSourceReads + 2) {
		if err := service.AppendLogChunk(context.Background(), AppendLogChunkRequest{
			RunID: runID, ClusterName: "colo", OffsetBytes: offset, Contents: []byte("x"),
		}); err != nil {
			t.Fatalf("AppendLogChunk(%d) error = %v", offset, err)
		}
	}

	stream, err := service.StreamLog(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("StreamLog() error = %v", err)
	}
	defer func() { _ = stream.Close() }()

	first, err := stream.Next()
	if err != nil || len(first.Contents) != maximumLogStreamSourceReads ||
		first.NextOffsetBytes != maximumLogStreamSourceReads {
		t.Fatalf("first Next() = (%#v, %v)", first, err)
	}
	second, err := stream.Next()
	if err != nil || len(second.Contents) != 2 ||
		second.OffsetBytes != maximumLogStreamSourceReads ||
		second.NextOffsetBytes != maximumLogStreamSourceReads+2 {
		t.Fatalf("second Next() = (%#v, %v)", second, err)
	}
}

func TestServiceFailsIncompleteLogIdempotently(t *testing.T) {
	t.Parallel()

	metadata := &serviceMetadataStore{runCluster: "colo", chunks: map[int64]LogChunk{}}
	contents := &serviceContentStore{}
	service := newTestService(metadata, contents)
	runID := uuid.New()
	prefix := []byte("durable prefix\n")
	if err := service.AppendLogChunk(context.Background(), AppendLogChunkRequest{
		RunID: runID, ClusterName: "colo", Contents: prefix,
	}); err != nil {
		t.Fatalf("AppendLogChunk() error = %v", err)
	}

	request := FailLogRequest{RunID: runID, ClusterName: "colo"}
	if err := service.FailLog(context.Background(), request); err != nil {
		t.Fatalf("FailLog() error = %v", err)
	}
	if err := service.FailLog(context.Background(), request); err != nil {
		t.Fatalf("repeated FailLog() error = %v", err)
	}
	cursor, err := service.GetLogCursor(context.Background(), runID, "colo")
	if err != nil {
		t.Fatalf("GetLogCursor() error = %v", err)
	}
	if cursor.State != StateFailed || cursor.NextOffsetBytes != int64(len(prefix)) || cursor.SHA256 != nil {
		t.Errorf("failed cursor = %#v", cursor)
	}
	if err := service.AppendLogChunk(context.Background(), AppendLogChunkRequest{
		RunID: runID, ClusterName: "colo", OffsetBytes: int64(len(prefix)), Contents: []byte("late"),
	}); !errors.Is(err, ErrLogClosed) {
		t.Errorf("AppendLogChunk() error = %v, want closed log", err)
	}

	stream, err := service.StreamLog(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("StreamLog() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunk, err := stream.Next()
	if err != nil || !slices.Equal(chunk.Contents, prefix) {
		t.Fatalf("partial log chunk = %#v, %v", chunk, err)
	}
	if _, err := stream.Next(); !errors.Is(err, ErrLogFailed) {
		t.Errorf("drained failed log error = %v, want log failed", err)
	}
}

func TestServiceLogStreamFollowsAppendAndCloseCancelsRead(t *testing.T) {
	t.Parallel()

	metadata := &serviceMetadataStore{runCluster: "colo", chunks: map[int64]LogChunk{}}
	contents := &serviceContentStore{}
	service := newTestService(metadata, contents)
	runID := uuid.New()
	if _, err := service.GetLogCursor(context.Background(), runID, "colo"); err != nil {
		t.Fatalf("GetLogCursor() error = %v", err)
	}
	stream, err := service.StreamLog(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("StreamLog() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	request := AppendLogChunkRequest{RunID: runID, ClusterName: "colo", Contents: []byte("live")}
	if err := service.AppendLogChunk(context.Background(), request); err != nil {
		t.Fatalf("AppendLogChunk() error = %v", err)
	}
	chunk, err := stream.Next()
	if err != nil || chunk.OffsetBytes != 0 || chunk.NextOffsetBytes != 4 || string(chunk.Contents) != "live" {
		t.Fatalf("Next() = (%#v, %v), want newly appended chunk", chunk, err)
	}
	request.OffsetBytes = 4
	if err := service.AppendLogChunk(context.Background(), request); err != nil {
		t.Fatalf("append next chunk: %v", err)
	}
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	blocked := &blockedLogContentStore{ContentStore: contents, body: reader, opened: make(chan struct{})}
	service.contents = blocked
	result := make(chan error, 1)
	go func() {
		_, err := stream.Next()
		result <- err
	}()
	select {
	case <-blocked.opened:
	case <-time.After(time.Second):
		t.Fatal("Next() did not open the next chunk")
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("closed Next() error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not interrupt the blocked read")
	}
}

type blockedLogContentStore struct {
	ContentStore
	body   io.ReadCloser
	opened chan struct{}
}

func (s *blockedLogContentStore) Open(context.Context, string) (objectstore.Object, error) {
	close(s.opened)

	return objectstore.Object{Body: s.body, Size: 4}, nil
}

func TestServiceUploadsDeclaredArtifactIdempotently(t *testing.T) {
	t.Parallel()

	for _, concurrentContents := range []string{"abc", "bad"} {
		t.Run("concurrent upload of "+concurrentContents, func(t *testing.T) {
			t.Parallel()

			metadata := &serviceMetadataStore{
				runCluster: "colo",
				context: RunContext{Artifacts: map[string]Declaration{
					"plan": {MediaType: "application/octet-stream", Sensitive: true},
				}},
			}
			firstUploadStarted := make(chan struct{})
			resumeFirstUpload := make(chan struct{})
			releaseFirstUpload := sync.OnceFunc(func() { close(resumeFirstUpload) })
			t.Cleanup(releaseFirstUpload)
			firstUpload := true
			contents := &serviceContentStore{beforePut: func() {
				if firstUpload {
					firstUpload = false
					close(firstUploadStarted)
					<-resumeFirstUpload
				}
			}}
			service := newTestService(metadata, contents)
			digest := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
			request := UploadRequest{
				RunID: uuid.New(), Name: "plan", SizeBytes: 3, SHA256: digest,
				Body: strings.NewReader(concurrentContents),
			}
			concurrentResult := make(chan error, 1)
			go func() { concurrentResult <- service.Upload(t.Context(), request) }()
			<-firstUploadStarted

			acceptedRequest := request
			acceptedRequest.Body = strings.NewReader("abc")
			if err := service.Upload(t.Context(), acceptedRequest); err != nil {
				t.Fatalf("Upload() error = %v", err)
			}
			releaseFirstUpload()
			concurrentErr := <-concurrentResult
			if concurrentContents == "abc" && concurrentErr != nil {
				t.Fatalf("concurrent identical Upload() error = %v", concurrentErr)
			}
			if concurrentContents == "bad" && !errors.Is(concurrentErr, ErrChunkConflict) {
				t.Fatalf("concurrent invalid Upload() error = %v", concurrentErr)
			}

			acceptedRequest.Body = strings.NewReader("ignored because metadata already exists")
			if err := service.Upload(t.Context(), acceptedRequest); err != nil {
				t.Fatalf("repeated Upload() error = %v", err)
			}
			if contents.puts != 2 || len(contents.objects) != 2 {
				t.Errorf("content puts = %d, objects = %d, want 2 each", contents.puts, len(contents.objects))
			}
			if metadata.record == nil || !metadata.record.Sensitive || metadata.record.SHA256 == nil ||
				*metadata.record.SHA256 != digest {
				t.Fatalf("artifact = %#v", metadata.record)
			}
			content, err := service.OpenArtifact(t.Context(), metadata.record.ID)
			if err != nil {
				t.Fatalf("OpenArtifact() error = %v", err)
			}
			body, readErr := io.ReadAll(content.Body)
			closeErr := content.Body.Close()
			if err := errors.Join(readErr, closeErr); err != nil {
				t.Fatalf("read artifact: %v", err)
			}
			if string(body) != "abc" {
				t.Errorf("accepted artifact contents = %q, want abc", body)
			}

			acceptedRequest.SizeBytes = 4
			if err := service.Upload(t.Context(), acceptedRequest); !errors.Is(err, ErrChunkConflict) {
				t.Errorf("conflicting Upload() error = %v", err)
			}
		})
	}
}

func TestServiceRejectsUnsafeRunReportArtifactDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		declaration Declaration
		sizeBytes   int64
		wantError   string
	}{
		{
			name: "not sensitive",
			declaration: Declaration{
				MediaType: runreport.TerraformPlanMediaType,
			},
			sizeBytes: 2,
			wantError: "must be sensitive",
		},
		{
			name: "too large",
			declaration: Declaration{
				MediaType: runreport.TerraformPlanMediaType,
				Sensitive: true,
			},
			sizeBytes: runreport.MaximumInfrastructurePlanSourceSizeBytes + 1,
			wantError: "source limit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			metadata := &serviceMetadataStore{
				runCluster: "colo",
				context: RunContext{Artifacts: map[string]Declaration{
					"plan-report": test.declaration,
				}},
			}
			contents := &serviceContentStore{}
			service := newTestService(metadata, contents)
			err := service.Upload(context.Background(), UploadRequest{
				RunID:     uuid.New(),
				Name:      "plan-report",
				Body:      strings.NewReader("{}"),
				SizeBytes: test.sizeBytes,
				SHA256:    strings.Repeat("a", 64),
			})
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Upload() error = %v, want invalid containing %q", err, test.wantError)
			}
			if contents.puts != 0 || metadata.record != nil {
				t.Errorf("rejected report persisted content or metadata: puts=%d record=%#v", contents.puts, metadata.record)
			}
		})
	}
}
