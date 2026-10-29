package runreport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var projectionDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ArtifactReport struct {
	ArtifactID uuid.UUID
	ReportEnvelope
	Summary             Summary
	ProjectionObjectKey string
	ProjectionSizeBytes int64
	ProjectionSHA256    string
	CreatedAt           time.Time
}

func NewArtifactReport(
	artifactID uuid.UUID,
	report Report,
	projectionObjectKey string,
	projection []byte,
	createdAt time.Time,
) (ArtifactReport, error) {
	if report == nil {
		return ArtifactReport{}, errors.New("run report is required")
	}

	digest := sha256.Sum256(projection)
	record := ArtifactReport{
		ArtifactID:          artifactID,
		ReportEnvelope:      report.Metadata(),
		Summary:             report.ReportSummary(),
		ProjectionObjectKey: projectionObjectKey,
		ProjectionSizeBytes: int64(len(projection)),
		ProjectionSHA256:    hex.EncodeToString(digest[:]),
		CreatedAt:           createdAt,
	}
	if err := record.Validate(); err != nil {
		return ArtifactReport{}, err
	}

	return record, nil
}

func (r ArtifactReport) Validate() error {
	if r.ArtifactID == uuid.Nil {
		return errors.New("artifact ID is required")
	}
	if err := r.ReportEnvelope.Validate(); err != nil {
		return err
	}
	if r.Summary == nil || r.Summary.ReportKind() != r.Kind {
		return errors.New("report summary must match the report kind")
	}
	if _, err := EncodeSummary(r.Summary); err != nil {
		return err
	}
	if strings.TrimSpace(r.ProjectionObjectKey) == "" || strings.HasPrefix(r.ProjectionObjectKey, "/") {
		return errors.New("projection object key must be a nonempty relative key")
	}
	if r.ProjectionSizeBytes <= 0 || r.ProjectionSizeBytes > MaximumProjectionSizeBytes {
		return fmt.Errorf("projection size must be between 1 and %d bytes", MaximumProjectionSizeBytes)
	}
	if !projectionDigestPattern.MatchString(r.ProjectionSHA256) {
		return errors.New("projection SHA-256 digest must be 64 lowercase hexadecimal characters")
	}
	if r.CreatedAt.IsZero() {
		return errors.New("report creation time is required")
	}

	return nil
}

func EncodeSummary(summary Summary) ([]byte, error) {
	if summary == nil {
		return nil, errors.New("run report summary is required")
	}
	if err := summary.Validate(); err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(summary)
	if err != nil {
		return nil, fmt.Errorf("encode Run report summary: %w", err)
	}
	if int64(len(encoded)) > maximumSummarySizeBytes {
		return nil, fmt.Errorf(
			"%w: summary must not exceed %d bytes",
			errSummaryTooLarge,
			maximumSummarySizeBytes,
		)
	}

	return encoded, nil
}

func DecodeSummary(kind ReportKind, encoded []byte) (Summary, error) {
	if int64(len(encoded)) > maximumSummarySizeBytes {
		return nil, fmt.Errorf(
			"%w: summary must not exceed %d bytes",
			errSummaryTooLarge,
			maximumSummarySizeBytes,
		)
	}

	adapter, found := adaptersByKind[kind]
	if !found {
		return nil, fmt.Errorf("run report kind %q is not supported", kind)
	}
	summary := adapter.newSummary()

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(summary); err != nil {
		return nil, fmt.Errorf("decode %s Run report summary: %w", kind, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("unexpected data after Run report summary")
		}

		return nil, fmt.Errorf("decode trailing Run report summary data: %w", err)
	}
	if err := summary.Validate(); err != nil {
		return nil, err
	}

	return summary, nil
}
