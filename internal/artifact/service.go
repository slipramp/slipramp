package artifact

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/objectstore"
)

const (
	MaxLogChunkSizeBytes = 256 * 1024
	logArtifactName      = "log"
	logMediaType         = "text/plain; charset=utf-8"
	logChunkMediaType    = "application/octet-stream"
)

type ContentStore interface {
	Put(
		ctx context.Context,
		key string,
		body io.Reader,
		size int64,
		options objectstore.PutOptions,
	) (int64, error)
	Open(ctx context.Context, key string) (objectstore.Object, error)
}

type AppendLogChunkRequest struct {
	RunID       uuid.UUID
	ClusterName string
	OffsetBytes int64
	Contents    []byte
}

func (r AppendLogChunkRequest) Validate() error {
	if r.RunID == uuid.Nil {
		return invalid("Run ID is required")
	}

	if err := cluster.ValidateName(r.ClusterName); err != nil {
		return invalid("%s", err)
	}

	if r.OffsetBytes < 0 {
		return invalid("chunk offset must not be negative")
	}

	if len(r.Contents) == 0 {
		return invalid("chunk contents must not be empty")
	}

	if len(r.Contents) > MaxLogChunkSizeBytes {
		return ErrChunkTooLarge
	}

	return nil
}

type Service struct {
	metadata Store
	contents ContentStore
	newID    func() uuid.UUID
	now      func() time.Time
	tokenKey []byte
}

func NewService(
	metadata Store,
	contents ContentStore,
	tokenKeys ...[]byte,
) *Service {
	tokenKey := []byte("slipramp-artifact-test-token-key")
	if len(tokenKeys) > 0 {
		tokenKey = bytes.Clone(tokenKeys[0])
	}

	return &Service{
		metadata: metadata,
		contents: contents,
		newID:    uuid.New,
		now:      time.Now,
		tokenKey: tokenKey,
	}
}

type LogCursor struct {
	NextOffsetBytes int64
	State           State
	SHA256          *string
}

type SealLogRequest struct {
	RunID       uuid.UUID
	ClusterName string
	SizeBytes   int64
	SHA256      string
}

type UploadRequest struct {
	RunID     uuid.UUID
	Name      string
	Body      io.Reader
	SizeBytes int64
	SHA256    string
}

type Content struct {
	Artifact Artifact
	Body     io.ReadCloser
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

func (s *Service) AppendLogChunk(
	ctx context.Context,
	request AppendLogChunkRequest,
) error {
	if err := request.Validate(); err != nil {
		return err
	}

	clusterName, err := s.metadata.GetRunClusterName(ctx, request.RunID)
	if errors.Is(err, ErrNotFound) ||
		(err == nil && clusterName != request.ClusterName) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get log Run cluster: %w", err)
	}

	record, err := s.ensureLogArtifact(ctx, request.RunID)
	if err != nil {
		return err
	}
	if record.Kind != KindLog {
		return fmt.Errorf("run log artifact has kind %q", record.Kind)
	}
	if record.State != StateOpen {
		return ErrLogClosed
	}

	digest := sha256.Sum256(request.Contents)
	encodedDigest := hex.EncodeToString(digest[:])
	objectKey := logChunkObjectKey(
		request.RunID,
		request.OffsetBytes,
		encodedDigest,
	)
	chunk := LogChunk{
		ArtifactID:  record.ID,
		OffsetBytes: request.OffsetBytes,
		SizeBytes:   int64(len(request.Contents)),
		SHA256:      encodedDigest,
		ObjectKey:   objectKey,
		CreatedAt:   s.now().UTC(),
	}

	existing, err := s.metadata.GetLogChunk(
		ctx,
		record.ID,
		request.OffsetBytes,
	)
	if err == nil {
		return compareLogChunks(existing, chunk)
	}
	if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("get existing log chunk: %w", err)
	}

	written, err := s.contents.Put(
		ctx,
		objectKey,
		bytes.NewReader(request.Contents),
		chunk.SizeBytes,
		objectstore.PutOptions{MediaType: logChunkMediaType},
	)
	if err != nil {
		return fmt.Errorf("store log chunk contents: %w", err)
	}
	if written != chunk.SizeBytes {
		return fmt.Errorf(
			"store log chunk contents: wrote %d bytes, expected %d",
			written,
			chunk.SizeBytes,
		)
	}

	_, err = s.metadata.CreateLogChunk(ctx, chunk)
	if errors.Is(err, ErrAlreadyExists) {
		existing, getErr := s.metadata.GetLogChunk(
			ctx,
			record.ID,
			request.OffsetBytes,
		)
		if getErr != nil {
			return fmt.Errorf("get concurrently stored log chunk: %w", getErr)
		}

		return compareLogChunks(existing, chunk)
	}
	if err != nil {
		return fmt.Errorf("record log chunk metadata: %w", err)
	}

	return nil
}

func (s *Service) GetLogCursor(
	ctx context.Context,
	runID uuid.UUID,
	clusterName string,
) (LogCursor, error) {
	if runID == uuid.Nil {
		return LogCursor{}, invalid("Run ID is required")
	}
	if err := cluster.ValidateName(clusterName); err != nil {
		return LogCursor{}, invalid("%s", err)
	}

	if err := s.authorizeClusterRun(ctx, runID, clusterName); err != nil {
		return LogCursor{}, err
	}

	record, err := s.ensureLogArtifact(ctx, runID)
	if err != nil {
		return LogCursor{}, err
	}

	return LogCursor{
		NextOffsetBytes: record.SizeBytes,
		State:           record.State,
		SHA256:          cloneString(record.SHA256),
	}, nil
}

func (s *Service) SealLog(ctx context.Context, request SealLogRequest) error {
	if request.RunID == uuid.Nil || request.SizeBytes < 0 ||
		!sha256Pattern.MatchString(request.SHA256) {
		return invalid("Run ID, nonnegative size, and SHA-256 digest are required")
	}
	if err := cluster.ValidateName(request.ClusterName); err != nil {
		return invalid("%s", err)
	}
	if err := s.authorizeClusterRun(
		ctx,
		request.RunID,
		request.ClusterName,
	); err != nil {
		return err
	}

	record, err := s.ensureLogArtifact(ctx, request.RunID)
	if err != nil {
		return err
	}
	if record.State == StateSealed {
		if record.SizeBytes == request.SizeBytes && record.SHA256 != nil &&
			*record.SHA256 == request.SHA256 {
			return nil
		}

		return ErrSealConflict
	}
	if record.State != StateOpen || record.SizeBytes != request.SizeBytes {
		return ErrSealConflict
	}

	digest := sha256.New()
	written, err := s.copyLogContents(ctx, record.ID, digest)
	if err != nil {
		return fmt.Errorf("read durable log contents: %w", err)
	}
	actualDigest := hex.EncodeToString(digest.Sum(nil))
	if written != request.SizeBytes || actualDigest != request.SHA256 {
		return ErrSealConflict
	}

	_, err = s.metadata.SealArtifact(
		ctx,
		record.ID,
		request.SizeBytes,
		request.SHA256,
		s.now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("seal log artifact: %w", err)
	}

	return nil
}

func (s *Service) Upload(
	ctx context.Context,
	request UploadRequest,
) error {
	if request.RunID == uuid.Nil || request.Body == nil || request.SizeBytes < 0 ||
		!sha256Pattern.MatchString(request.SHA256) {
		return invalid("Run ID, body, nonnegative size, and SHA-256 digest are required")
	}

	runContext, err := s.metadata.GetRunArtifactContext(ctx, request.RunID)
	if err != nil {
		return err
	}
	declaration, found := runContext.Artifacts[request.Name]
	if !found {
		return ErrNotFound
	}

	existing, err := s.metadata.GetArtifactByRunAndName(
		ctx,
		request.RunID,
		request.Name,
	)
	if err == nil {
		return compareUpload(existing, request)
	}
	if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("get existing output artifact: %w", err)
	}

	mediaType := declaration.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	objectKey := outputObjectKey(request.RunID, request.Name, request.SHA256)
	digest := sha256.New()
	written, err := s.contents.Put(
		ctx,
		objectKey,
		io.TeeReader(request.Body, digest),
		request.SizeBytes,
		objectstore.PutOptions{MediaType: mediaType},
	)
	if err != nil {
		return fmt.Errorf("store output artifact contents: %w", err)
	}
	actualDigest := hex.EncodeToString(digest.Sum(nil))
	if written != request.SizeBytes || actualDigest != request.SHA256 {
		return ErrChunkConflict
	}

	sealedAt := s.now().UTC()
	record := Artifact{
		ID:             s.newID(),
		RunID:          request.RunID,
		Name:           request.Name,
		Kind:           KindOutput,
		State:          StateSealed,
		MediaType:      mediaType,
		SizeBytes:      request.SizeBytes,
		SHA256:         cloneString(&request.SHA256),
		Sensitive:      declaration.Sensitive,
		ObjectKey:      &objectKey,
		RetentionState: RetentionRetained,
		CreatedAt:      sealedAt,
		SealedAt:       &sealedAt,
	}
	_, err = s.metadata.CreateArtifact(ctx, record)
	if errors.Is(err, ErrAlreadyExists) {
		existing, getErr := s.metadata.GetArtifactByRunAndName(
			ctx,
			request.RunID,
			request.Name,
		)
		if getErr != nil {
			return fmt.Errorf("get concurrently uploaded artifact: %w", getErr)
		}

		return compareUpload(existing, request)
	}
	if err != nil {
		return fmt.Errorf("record output artifact: %w", err)
	}

	return nil
}

func (s *Service) List(
	ctx context.Context,
	runID uuid.UUID,
) ([]Record, error) {
	if _, err := s.metadata.GetRunClusterName(ctx, runID); err != nil {
		return nil, err
	}

	artifacts, err := s.metadata.ListArtifactsForRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list Run artifacts: %w", err)
	}
	provenance, err := s.metadata.GetArtifactProvenance(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("get artifact provenance: %w", err)
	}

	result := make([]Record, 0, len(artifacts))
	for _, record := range artifacts {
		result = append(result, Record{Artifact: record, Provenance: provenance})
	}

	return result, nil
}

func (s *Service) OpenArtifact(
	ctx context.Context,
	artifactID uuid.UUID,
) (Content, error) {
	record, err := s.metadata.GetArtifact(ctx, artifactID)
	if err != nil {
		return Content{}, err
	}

	return s.openRecord(ctx, record)
}

func (s *Service) OpenInput(
	ctx context.Context,
	runID uuid.UUID,
	inputName string,
) (Content, error) {
	record, err := s.metadata.GetRunInputArtifact(ctx, runID, inputName)
	if err != nil {
		return Content{}, err
	}

	return s.openRecord(ctx, record)
}

func (s *Service) StreamLog(
	ctx context.Context,
	runID uuid.UUID,
	offsetBytes int64,
) (io.ReadCloser, error) {
	if offsetBytes < 0 {
		return nil, invalid("log offset must not be negative")
	}
	record, err := s.metadata.GetArtifactByRunAndName(ctx, runID, logArtifactName)
	if err != nil {
		return nil, err
	}
	if offsetBytes > record.SizeBytes {
		return nil, invalid("log offset exceeds durable size")
	}

	reader, writer := io.Pipe()
	go s.writeLogEvents(ctx, writer, record.ID, offsetBytes)

	return reader, nil
}

func (s *Service) ensureLogArtifact(
	ctx context.Context,
	runID uuid.UUID,
) (Artifact, error) {
	record, err := s.metadata.GetArtifactByRunAndName(ctx, runID, logArtifactName)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Artifact{}, fmt.Errorf("get Run log artifact: %w", err)
	}

	record = Artifact{
		ID:             s.newID(),
		RunID:          runID,
		Name:           logArtifactName,
		Kind:           KindLog,
		State:          StateOpen,
		MediaType:      logMediaType,
		RetentionState: RetentionRetained,
		CreatedAt:      s.now().UTC(),
	}
	created, err := s.metadata.CreateArtifact(ctx, record)
	if errors.Is(err, ErrAlreadyExists) {
		existing, getErr := s.metadata.GetArtifactByRunAndName(
			ctx,
			runID,
			logArtifactName,
		)
		if getErr != nil {
			return Artifact{}, fmt.Errorf("get concurrently created Run log artifact: %w", getErr)
		}

		return existing, nil
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("create Run log artifact: %w", err)
	}

	return created, nil
}

func compareLogChunks(existing, candidate LogChunk) error {
	if existing.SizeBytes == candidate.SizeBytes &&
		existing.SHA256 == candidate.SHA256 &&
		existing.ObjectKey == candidate.ObjectKey {
		return nil
	}

	return ErrChunkConflict
}

func logChunkObjectKey(
	runID uuid.UUID,
	offsetBytes int64,
	digest string,
) string {
	return strings.Join(
		[]string{
			"runs",
			runID.String(),
			"artifacts",
			logArtifactName,
			"chunks",
			fmt.Sprintf("%d-%s", offsetBytes, digest),
		},
		"/",
	)
}

func outputObjectKey(runID uuid.UUID, name, digest string) string {
	return strings.Join(
		[]string{"runs", runID.String(), "artifacts", name, "content-" + digest},
		"/",
	)
}

func (s *Service) authorizeClusterRun(
	ctx context.Context,
	runID uuid.UUID,
	clusterName string,
) error {
	assignedCluster, err := s.metadata.GetRunClusterName(ctx, runID)
	if errors.Is(err, ErrNotFound) ||
		(err == nil && assignedCluster != clusterName) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get log Run cluster: %w", err)
	}

	return nil
}

func compareUpload(existing Artifact, request UploadRequest) error {
	if existing.State == StateSealed && existing.SizeBytes == request.SizeBytes &&
		existing.SHA256 != nil && *existing.SHA256 == request.SHA256 {
		return nil
	}

	return ErrChunkConflict
}

func (s *Service) openRecord(
	ctx context.Context,
	record Artifact,
) (Content, error) {
	if record.RetentionState != RetentionRetained {
		return Content{}, ErrNotFound
	}
	if record.State != StateSealed || record.SHA256 == nil {
		return Content{}, ErrNotSealed
	}

	if record.Kind == KindOutput {
		if record.ObjectKey == nil {
			return Content{}, fmt.Errorf("sealed output artifact has no object key")
		}
		object, err := s.contents.Open(ctx, *record.ObjectKey)
		if err != nil {
			return Content{}, fmt.Errorf("open artifact contents: %w", err)
		}
		if object.Size != record.SizeBytes {
			_ = object.Body.Close()
			return Content{}, fmt.Errorf("artifact content size does not match metadata")
		}

		return Content{Artifact: record, Body: object.Body}, nil
	}

	reader, writer := io.Pipe()
	go func() {
		_, err := s.copyLogContents(ctx, record.ID, writer)
		_ = writer.CloseWithError(err)
	}()

	return Content{Artifact: record, Body: reader}, nil
}

func (s *Service) copyLogContents(
	ctx context.Context,
	artifactID uuid.UUID,
	destination io.Writer,
) (int64, error) {
	chunks, err := s.metadata.ListLogChunks(ctx, artifactID, 0)
	if err != nil {
		return 0, err
	}

	var offset int64
	for _, chunk := range chunks {
		if chunk.OffsetBytes != offset {
			return offset, fmt.Errorf("log chunk gap at byte %d", offset)
		}
		object, err := s.contents.Open(ctx, chunk.ObjectKey)
		if err != nil {
			return offset, err
		}
		written, copyErr := io.Copy(destination, object.Body)
		closeErr := object.Body.Close()
		if copyErr != nil {
			return offset, copyErr
		}
		if closeErr != nil {
			return offset, closeErr
		}
		if written != chunk.SizeBytes {
			return offset, fmt.Errorf(
				"log chunk at byte %d has size %d, expected %d",
				chunk.OffsetBytes,
				written,
				chunk.SizeBytes,
			)
		}
		offset = chunk.EndOffset()
	}

	return offset, nil
}

func (s *Service) writeLogEvents(
	ctx context.Context,
	writer *io.PipeWriter,
	artifactID uuid.UUID,
	offsetBytes int64,
) {
	defer func() { _ = writer.Close() }()

	cursor := offsetBytes
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		chunks, err := s.metadata.ListLogChunks(ctx, artifactID, cursor)
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		for _, chunk := range chunks {
			if chunk.EndOffset() <= cursor {
				continue
			}
			object, err := s.contents.Open(ctx, chunk.ObjectKey)
			if err != nil {
				_ = writer.CloseWithError(err)
				return
			}
			contents, readErr := io.ReadAll(object.Body)
			closeErr := object.Body.Close()
			if readErr != nil || closeErr != nil {
				_ = writer.CloseWithError(errors.Join(readErr, closeErr))
				return
			}
			if int64(len(contents)) != chunk.SizeBytes {
				_ = writer.CloseWithError(fmt.Errorf(
					"log chunk at byte %d has size %d, expected %d",
					chunk.OffsetBytes,
					len(contents),
					chunk.SizeBytes,
				))
				return
			}

			start := max(cursor-chunk.OffsetBytes, 0)
			if start > int64(len(contents)) {
				_ = writer.CloseWithError(errors.New("log cursor exceeds chunk contents"))
				return
			}
			contents = contents[start:]
			event := struct {
				OffsetBytes     int64  `json:"offsetBytes"`
				NextOffsetBytes int64  `json:"nextOffsetBytes"`
				ContentsBase64  string `json:"contentsBase64"`
			}{
				OffsetBytes:     cursor,
				NextOffsetBytes: chunk.EndOffset(),
				ContentsBase64:  base64.StdEncoding.EncodeToString(contents),
			}
			encoded, _ := json.Marshal(event)
			if _, err := fmt.Fprintf(
				writer,
				"id: %d\nevent: chunk\ndata: %s\n\n",
				event.NextOffsetBytes,
				encoded,
			); err != nil {
				return
			}
			cursor = chunk.EndOffset()
		}

		record, err := s.metadata.GetArtifact(ctx, artifactID)
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		if record.State != StateOpen && cursor >= record.SizeBytes {
			_, _ = fmt.Fprintf(
				writer,
				"id: %d\nevent: sealed\ndata: {}\n\n",
				cursor,
			)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
