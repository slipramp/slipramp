package artifact

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

type Content struct {
	Artifact Artifact
	Body     io.ReadCloser
}

func (s *Service) List(ctx context.Context, runID uuid.UUID) ([]Record, error) {
	runContext, err := s.runs.GetRunArtifactContext(ctx, runID)
	if err != nil {
		return nil, err
	}

	artifacts, err := s.artifacts.ListArtifactsForRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list Run artifacts: %w", err)
	}
	provenance, err := s.runs.GetArtifactProvenance(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("get artifact provenance: %w", err)
	}

	result := make([]Record, 0, len(artifacts))
	for _, record := range artifacts {
		declaration := runContext.Artifacts[record.Name]
		result = append(result, Record{
			Artifact:    record,
			Description: declaration.Description,
			Provenance:  provenance,
		})
	}

	return result, nil
}

func (s *Service) OpenArtifact(ctx context.Context, artifactID uuid.UUID) (Content, error) {
	record, err := s.artifacts.GetArtifact(ctx, artifactID)
	if err != nil {
		return Content{}, err
	}

	return s.openRecord(ctx, record)
}

func (s *Service) OpenInput(ctx context.Context, runID uuid.UUID, inputName string) (Content, error) {
	record, err := s.runs.GetRunInputArtifact(ctx, runID, inputName)
	if err != nil {
		return Content{}, err
	}

	return s.openRecord(ctx, record)
}

func (s *Service) openRecord(ctx context.Context, record Artifact) (Content, error) {
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
