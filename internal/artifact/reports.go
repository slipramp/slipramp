package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/slipramp/slipramp/internal/objectstore"
	"github.com/slipramp/slipramp/internal/runreport"
)

type ReportIngestionOutcome string

const (
	ReportIngestionSucceeded ReportIngestionOutcome = "succeeded"
	ReportIngestionRejected  ReportIngestionOutcome = "rejected"
	ReportIngestionFailed    ReportIngestionOutcome = "failed"
)

type ReportIngestionObserver interface {
	ObserveReportIngestion(context.Context, runreport.ReportKind, ReportIngestionOutcome, time.Duration)
}

type noopReportIngestionObserver struct{}

func (noopReportIngestionObserver) ObserveReportIngestion(
	context.Context,
	runreport.ReportKind,
	ReportIngestionOutcome,
	time.Duration,
) {
}

func reportIngestionObserverOrNoop(observer ReportIngestionObserver) ReportIngestionObserver {
	if observer == nil {
		return noopReportIngestionObserver{}
	}

	return observer
}

func reportIngestionOutcome(err error) ReportIngestionOutcome {
	switch {
	case err == nil:
		return ReportIngestionSucceeded
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrChunkConflict):
		return ReportIngestionRejected
	default:
		return ReportIngestionFailed
	}
}

func (s *Service) projectReport(
	ctx context.Context,
	adapter runreport.Adapter,
	record Artifact,
) (runreport.ArtifactReport, error) {
	if record.ObjectKey == nil {
		return runreport.ArtifactReport{}, errors.New("report source Artifact has no object key")
	}

	source, err := s.contents.Open(ctx, *record.ObjectKey)
	if err != nil {
		return runreport.ArtifactReport{}, fmt.Errorf("open stored Run report source: %w", err)
	}
	if source.Size != record.SizeBytes {
		_ = source.Body.Close()

		return runreport.ArtifactReport{}, errors.New("stored Run report source size does not match metadata")
	}
	report, decodeErr := adapter.Decode(source.Body)
	closeErr := source.Body.Close()
	if decodeErr != nil {
		if errors.Is(decodeErr, runreport.ErrSourceRead) {
			return runreport.ArtifactReport{}, decodeErr
		}

		return runreport.ArtifactReport{}, invalid("Run report source is invalid: %v", decodeErr)
	}
	if closeErr != nil {
		return runreport.ArtifactReport{}, fmt.Errorf("close stored Run report source: %w", closeErr)
	}

	projection, err := runreport.Encode(report)
	if err != nil {
		return runreport.ArtifactReport{}, invalid("Run report projection is invalid: %v", err)
	}
	digest := sha256.Sum256(projection)
	encodedDigest := hex.EncodeToString(digest[:])
	metadata, err := runreport.NewArtifactReport(
		record.ID,
		report,
		reportProjectionObjectKey(record, report.Metadata(), encodedDigest),
		projection,
		record.CreatedAt,
	)
	if err != nil {
		return runreport.ArtifactReport{}, fmt.Errorf("build Run report metadata: %w", err)
	}
	written, err := s.contents.Put(
		ctx,
		metadata.ProjectionObjectKey,
		bytes.NewReader(projection),
		metadata.ProjectionSizeBytes,
		objectstore.PutOptions{MediaType: "application/json"},
	)
	if err != nil {
		return runreport.ArtifactReport{}, fmt.Errorf("store Run report projection: %w", err)
	}
	if written != metadata.ProjectionSizeBytes {
		return runreport.ArtifactReport{}, errors.New("stored Run report projection size does not match metadata")
	}

	return metadata, nil
}

func reportProjectionObjectKey(
	record Artifact,
	envelope runreport.ReportEnvelope,
	digest string,
) string {
	filename := fmt.Sprintf("%s-v%d-%s.json", envelope.Kind, envelope.SchemaVersion, digest)

	return fmt.Sprintf(
		"runs/%s/artifacts/%s/reports/%s",
		record.RunID,
		record.Name,
		filename,
	)
}
