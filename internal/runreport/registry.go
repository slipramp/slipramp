// Package runreport provides the registered Run report ingestion boundary and persisted metadata.
package runreport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"

	"github.com/slipramp/slipramp/internal/runreport/ansible"
	"github.com/slipramp/slipramp/internal/runreport/infrastructure"
	"github.com/slipramp/slipramp/internal/runreport/model"
	"github.com/slipramp/slipramp/internal/runreport/pyinfra"
)

type registration struct {
	mediaType string
	format    model.Format
}

var registry = [...]registration{
	{mediaType: infrastructure.TerraformPlanMediaType, format: infrastructure.TerraformPlanFormat()},
	{mediaType: infrastructure.OpenTofuPlanMediaType, format: infrastructure.OpenTofuPlanFormat()},
	{mediaType: pyinfra.DeployMediaType, format: pyinfra.DeployFormat()},
	{mediaType: ansible.PlaybookMediaType, format: ansible.PlaybookFormat()},
}

func AdapterForMediaType(value string) (model.Adapter, bool) {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return nil, false
	}

	for _, registered := range registry {
		if registered.mediaType == mediaType {
			return registered.format, true
		}
	}

	return nil, false
}

func IsRecognizedMediaType(value string) bool {
	_, found := AdapterForMediaType(value)

	return found
}

func DecodeProjection(kind model.Kind, source io.Reader) (model.Report, error) {
	if source == nil {
		return nil, errors.New("run report projection is required")
	}

	contents, err := io.ReadAll(io.LimitReader(source, model.MaximumProjectionSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", model.ErrProjectionRead, err)
	}
	if int64(len(contents)) > model.MaximumProjectionSizeBytes {
		return nil, fmt.Errorf(
			"%w: projection must not exceed %d bytes",
			model.ErrProjectionTooLarge,
			model.MaximumProjectionSizeBytes,
		)
	}

	format, found := formatForKind(kind)
	if !found {
		return nil, fmt.Errorf("run report kind %q is not supported", kind)
	}
	report := format.NewReport()

	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(report); err != nil {
		return nil, fmt.Errorf("decode %s Run report projection: %w", kind, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("unexpected data after Run report projection")
		}

		return nil, fmt.Errorf("decode trailing Run report projection data: %w", err)
	}

	if _, err := model.Encode(report); err != nil {
		return nil, err
	}

	return report, nil
}

func DecodeSummary(kind model.Kind, encoded []byte) (model.Summary, error) {
	if int64(len(encoded)) > model.MaximumSummarySizeBytes {
		return nil, fmt.Errorf(
			"%w: summary must not exceed %d bytes",
			model.ErrSummaryTooLarge,
			model.MaximumSummarySizeBytes,
		)
	}

	format, found := formatForKind(kind)
	if !found {
		return nil, fmt.Errorf("run report kind %q is not supported", kind)
	}
	summary := format.NewSummary()

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

func ValidateEnvelope(envelope model.Envelope) error {
	format, found := formatForKind(envelope.Kind)
	if !found {
		return fmt.Errorf("run report kind %q is not supported", envelope.Kind)
	}

	return format.ValidateEnvelope(envelope)
}

func formatForKind(kind model.Kind) (model.Format, bool) {
	for _, registered := range registry {
		if registered.format.Kind() == kind {
			return registered.format, true
		}
	}

	return nil, false
}
