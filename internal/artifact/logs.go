package artifact

import (
	"bytes"
	"context"
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

func (s *Service) AppendLogChunk(ctx context.Context, request AppendLogChunkRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}

	clusterName, err := s.runs.GetRunClusterName(ctx, request.RunID)
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
	objectKey := logChunkObjectKey(request.RunID, request.OffsetBytes, encodedDigest)
	chunk := LogChunk{
		ArtifactID:  record.ID,
		OffsetBytes: request.OffsetBytes,
		SizeBytes:   int64(len(request.Contents)),
		SHA256:      encodedDigest,
		ObjectKey:   objectKey,
		CreatedAt:   s.now().UTC(),
	}

	existing, err := s.logChunks.GetLogChunk(ctx, record.ID, request.OffsetBytes)
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
		return fmt.Errorf("store log chunk contents: wrote %d bytes, expected %d", written, chunk.SizeBytes)
	}

	_, err = s.logChunks.CreateLogChunk(ctx, chunk)
	if errors.Is(err, ErrAlreadyExists) {
		existing, getErr := s.logChunks.GetLogChunk(ctx, record.ID, request.OffsetBytes)
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

func (s *Service) GetLogCursor(ctx context.Context, runID uuid.UUID, clusterName string) (LogCursor, error) {
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
	if err := s.authorizeClusterRun(ctx, request.RunID, request.ClusterName); err != nil {
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

	_, err = s.artifacts.SealArtifact(ctx, record.ID, request.SizeBytes, request.SHA256, s.now().UTC())
	if err != nil {
		return fmt.Errorf("seal log artifact: %w", err)
	}

	return nil
}

func (s *Service) StreamLog(ctx context.Context, runID uuid.UUID, offsetBytes int64) (io.ReadCloser, error) {
	if offsetBytes < 0 {
		return nil, invalid("log offset must not be negative")
	}
	record, err := s.artifacts.GetArtifactByRunAndName(ctx, runID, logArtifactName)
	if err != nil {
		return nil, err
	}
	if record.RetentionState != RetentionRetained {
		return nil, ErrNotFound
	}
	if offsetBytes > record.SizeBytes {
		return nil, invalid("log offset exceeds durable size")
	}

	reader, writer := io.Pipe()
	go s.writeLogEvents(ctx, writer, record.ID, offsetBytes)

	return reader, nil
}

func (s *Service) ensureLogArtifact(ctx context.Context, runID uuid.UUID) (Artifact, error) {
	record, err := s.artifacts.GetArtifactByRunAndName(ctx, runID, logArtifactName)
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
	created, err := s.artifacts.CreateArtifact(ctx, record)
	if errors.Is(err, ErrAlreadyExists) {
		existing, getErr := s.artifacts.GetArtifactByRunAndName(ctx, runID, logArtifactName)
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

func logChunkObjectKey(runID uuid.UUID, offsetBytes int64, digest string) string {
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

func (s *Service) authorizeClusterRun(ctx context.Context, runID uuid.UUID, clusterName string) error {
	assignedCluster, err := s.runs.GetRunClusterName(ctx, runID)
	if errors.Is(err, ErrNotFound) ||
		(err == nil && assignedCluster != clusterName) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get log Run cluster: %w", err)
	}

	return nil
}

func (s *Service) copyLogContents(ctx context.Context, artifactID uuid.UUID, destination io.Writer) (int64, error) {
	chunks, err := s.logChunks.ListLogChunks(ctx, artifactID, 0)
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

func (s *Service) writeLogEvents(ctx context.Context, writer *io.PipeWriter, artifactID uuid.UUID, offsetBytes int64) {
	defer func() { _ = writer.Close() }()

	cursor := offsetBytes
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		chunks, err := s.logChunks.ListLogChunks(ctx, artifactID, cursor)
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

		record, err := s.artifacts.GetArtifact(ctx, artifactID)
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		if record.State != StateOpen && cursor >= record.SizeBytes {
			_, _ = fmt.Fprintf(writer, "id: %d\nevent: sealed\ndata: {}\n\n", cursor)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
