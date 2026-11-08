// Package model defines the format-neutral Run report contract.
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	PreviewMode Mode = "preview"
	ApplyMode   Mode = "apply"

	MaximumProjectionSizeBytes int64 = 16 * 1024 * 1024
	MaximumSummarySizeBytes    int64 = 64 * 1024
)

var (
	ErrSourceTooLarge     = errors.New("run report source exceeds the maximum size")
	ErrSourceRead         = errors.New("read run report source")
	ErrProjectionRead     = errors.New("read run report projection")
	ErrProjectionTooLarge = errors.New("run report projection exceeds the maximum size")
	ErrSummaryTooLarge    = errors.New("run report summary exceeds the maximum size")
)

type Kind string

type Mode string

type Envelope struct {
	Kind           Kind   `json:"kind"`
	SchemaVersion  int    `json:"schemaVersion"`
	AdapterVersion int    `json:"adapterVersion"`
	Tool           string `json:"tool"`
	ToolVersion    string `json:"toolVersion,omitempty"`
	Mode           Mode   `json:"mode"`
}

type Report interface {
	Metadata() Envelope
	ReportSummary() Summary
	Validate() error
}

type Summary interface {
	ReportKind() Kind
	Validate() error
}

type Adapter interface {
	Kind() Kind
	MaximumSourceSizeBytes() int64
	Decode(io.Reader) (Report, error)
}

// Format extends the ingestion adapter with constructors and envelope validation
// used by the registry when loading persisted projections and summaries.
type Format interface {
	Adapter
	NewReport() Report
	NewSummary() Summary
	ValidateEnvelope(Envelope) error
}

func Encode(report Report) ([]byte, error) {
	if report == nil {
		return nil, errors.New("run report is required")
	}
	if err := report.Validate(); err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("encode Run report projection: %w", err)
	}
	if int64(len(encoded)) > MaximumProjectionSizeBytes {
		return nil, fmt.Errorf(
			"%w: projection must not exceed %d bytes",
			ErrProjectionTooLarge,
			MaximumProjectionSizeBytes,
		)
	}

	return encoded, nil
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
	if int64(len(encoded)) > MaximumSummarySizeBytes {
		return nil, fmt.Errorf(
			"%w: summary must not exceed %d bytes",
			ErrSummaryTooLarge,
			MaximumSummarySizeBytes,
		)
	}

	return encoded, nil
}

func ValidateRequiredString(field string, value string, maximum int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}

	return ValidateOptionalString(field, value, maximum)
}

func ValidateOptionalString(field string, value string, maximum int) error {
	if utf8.RuneCountInString(value) > maximum {
		return fmt.Errorf("%s must not exceed %d characters", field, maximum)
	}

	return nil
}
