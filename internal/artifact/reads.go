package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/runreport"
)

type Content struct {
	Artifact Artifact
	Body     io.ReadCloser
}

type ReportContent struct {
	Artifact Artifact
	Metadata runreport.ArtifactReport
	Report   runreport.Report
}

func (s *Service) GetStorageUsage(ctx context.Context) (StorageUsage, error) {
	usage, err := s.artifacts.GetArtifactStorageUsage(ctx)
	if err != nil {
		return StorageUsage{}, fmt.Errorf("get artifact storage usage: %w", err)
	}

	return usage, nil
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
	reports, err := s.artifacts.ListArtifactReportsForRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list Run artifact reports: %w", err)
	}
	reportsByArtifact := make(map[uuid.UUID]*runreport.ArtifactReport, len(reports))
	for index := range reports {
		reportsByArtifact[reports[index].ArtifactID] = &reports[index]
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
			Report:      reportsByArtifact[record.ID],
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

func (s *Service) OpenReport(ctx context.Context, artifactID uuid.UUID) (ReportContent, error) {
	record, err := s.artifacts.GetArtifact(ctx, artifactID)
	if err != nil {
		return ReportContent{}, err
	}
	if record.RetentionState != RetentionRetained {
		return ReportContent{}, ErrNotFound
	}
	if record.State != StateSealed {
		return ReportContent{}, ErrNotSealed
	}

	metadata, err := s.artifacts.GetArtifactReport(ctx, artifactID)
	if err != nil {
		return ReportContent{}, err
	}
	projection, err := s.contents.Open(ctx, metadata.ProjectionObjectKey)
	if err != nil {
		return ReportContent{}, fmt.Errorf("open Run report projection: %w", err)
	}
	if projection.Size != metadata.ProjectionSizeBytes {
		_ = projection.Body.Close()

		return ReportContent{}, errors.New("Run report projection size does not match metadata")
	}

	contents, readErr := io.ReadAll(io.LimitReader(projection.Body, metadata.ProjectionSizeBytes+1))
	closeErr := projection.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return ReportContent{}, fmt.Errorf("read Run report projection: %w", err)
	}
	if int64(len(contents)) != metadata.ProjectionSizeBytes {
		return ReportContent{}, errors.New("Run report projection size does not match metadata")
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != metadata.ProjectionSHA256 {
		return ReportContent{}, errors.New("Run report projection digest does not match metadata")
	}

	report, err := runreport.DecodeProjection(metadata.Kind, bytes.NewReader(contents))
	if err != nil {
		return ReportContent{}, fmt.Errorf("decode Run report projection: %w", err)
	}
	if report.Metadata() != metadata.ReportEnvelope {
		return ReportContent{}, errors.New("Run report projection envelope does not match metadata")
	}
	projectedSummary, err := runreport.EncodeSummary(report.ReportSummary())
	if err != nil {
		return ReportContent{}, fmt.Errorf("encode Run report projection summary: %w", err)
	}
	storedSummary, err := runreport.EncodeSummary(metadata.Summary)
	if err != nil {
		return ReportContent{}, fmt.Errorf("encode stored Run report summary: %w", err)
	}
	if !bytes.Equal(projectedSummary, storedSummary) {
		return ReportContent{}, errors.New("Run report projection summary does not match metadata")
	}

	return ReportContent{Artifact: record, Metadata: metadata, Report: report}, nil
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
