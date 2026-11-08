package runreport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/runreport/model"
)

var projectionDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ArtifactReport struct {
	ArtifactID uuid.UUID
	model.Envelope
	Summary             model.Summary
	ProjectionObjectKey string
	ProjectionSizeBytes int64
	ProjectionSHA256    string
	CreatedAt           time.Time
}

func NewArtifactReport(
	artifactID uuid.UUID,
	report model.Report,
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
		Envelope:            report.Metadata(),
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
	if err := ValidateEnvelope(r.Envelope); err != nil {
		return err
	}
	if r.Summary == nil || r.Summary.ReportKind() != r.Kind {
		return errors.New("report summary must match the report kind")
	}
	if _, err := model.EncodeSummary(r.Summary); err != nil {
		return err
	}
	if strings.TrimSpace(r.ProjectionObjectKey) == "" || strings.HasPrefix(r.ProjectionObjectKey, "/") {
		return errors.New("projection object key must be a nonempty relative key")
	}
	if r.ProjectionSizeBytes <= 0 || r.ProjectionSizeBytes > model.MaximumProjectionSizeBytes {
		return fmt.Errorf("projection size must be between 1 and %d bytes", model.MaximumProjectionSizeBytes)
	}
	if !projectionDigestPattern.MatchString(r.ProjectionSHA256) {
		return errors.New("projection SHA-256 digest must be 64 lowercase hexadecimal characters")
	}
	if r.CreatedAt.IsZero() {
		return errors.New("report creation time is required")
	}

	return nil
}
