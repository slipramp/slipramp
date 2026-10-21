package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/objectstore"
)

type UploadRequest struct {
	RunID     uuid.UUID
	Name      string
	Body      io.Reader
	SizeBytes int64
	SHA256    string
}

func (s *Service) Upload(ctx context.Context, request UploadRequest) error {
	if request.RunID == uuid.Nil || request.Body == nil || request.SizeBytes < 0 ||
		!sha256Pattern.MatchString(request.SHA256) {
		return invalid("Run ID, body, nonnegative size, and SHA-256 digest are required")
	}

	runContext, err := s.runs.GetRunArtifactContext(ctx, request.RunID)
	if err != nil {
		return err
	}
	declaration, found := runContext.Artifacts[request.Name]
	if !found {
		return ErrNotFound
	}

	existing, err := s.artifacts.GetArtifactByRunAndName(ctx, request.RunID, request.Name)
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
	_, err = s.artifacts.CreateArtifact(ctx, record)
	if errors.Is(err, ErrAlreadyExists) {
		existing, getErr := s.artifacts.GetArtifactByRunAndName(ctx, request.RunID, request.Name)
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

func outputObjectKey(runID uuid.UUID, name, digest string) string {
	return strings.Join([]string{"runs", runID.String(), "artifacts", name, "content-" + digest}, "/")
}

func compareUpload(existing Artifact, request UploadRequest) error {
	if existing.State == StateSealed && existing.SizeBytes == request.SizeBytes &&
		existing.SHA256 != nil && *existing.SHA256 == request.SHA256 {
		return nil
	}

	return ErrChunkConflict
}
