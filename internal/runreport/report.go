// Package runreport defines bounded, immutable projections of structured Run artifacts.
package runreport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
)

const (
	TerraformPlanMediaType = "application/vnd.slipramp.terraform-plan+json"
	OpenTofuPlanMediaType  = "application/vnd.slipramp.opentofu-plan+json"
	PyinfraDeployMediaType = "application/vnd.slipramp.pyinfra-deploy+json"

	TerraformPlanKind ReportKind = "terraform-plan"
	OpenTofuPlanKind  ReportKind = "opentofu-plan"
	PyinfraDeployKind ReportKind = "pyinfra-deploy"
	PreviewMode       ReportMode = "preview"
	ApplyMode         ReportMode = "apply"

	InfrastructurePlanSchemaVersion  = 1
	InfrastructurePlanAdapterVersion = 1
	PyinfraDeploySchemaVersion       = 1
	PyinfraDeployAdapterVersion      = 1

	MaximumInfrastructurePlanSourceSizeBytes int64 = 32 * 1024 * 1024
	MaximumPyinfraDeploySourceSizeBytes      int64 = 32 * 1024 * 1024
	MaximumProjectionSizeBytes               int64 = 16 * 1024 * 1024
	maximumSummarySizeBytes                  int64 = 64 * 1024
)

var (
	ErrSourceTooLarge     = errors.New("run report source exceeds the maximum size")
	ErrSourceRead         = errors.New("read run report source")
	ErrProjectionRead     = errors.New("read run report projection")
	ErrProjectionTooLarge = errors.New("run report projection exceeds the maximum size")
	errSummaryTooLarge    = errors.New("run report summary exceeds the maximum size")
)

type ReportKind string

type ReportMode string

type ReportEnvelope struct {
	Kind           ReportKind `json:"kind"`
	SchemaVersion  int        `json:"schemaVersion"`
	AdapterVersion int        `json:"adapterVersion"`
	Tool           string     `json:"tool"`
	ToolVersion    string     `json:"toolVersion,omitempty"`
	Mode           ReportMode `json:"mode"`
}

type Report interface {
	Metadata() ReportEnvelope
	ReportSummary() Summary
	Validate() error
}

type Summary interface {
	ReportKind() ReportKind
	Validate() error
}

type Adapter interface {
	Kind() ReportKind
	MaximumSourceSizeBytes() int64
	Decode(io.Reader) (Report, error)
}

type registeredAdapter interface {
	Adapter
	newReport() Report
	newSummary() Summary
	validateEnvelope(ReportEnvelope) error
}

var terraformPlanReportAdapter registeredAdapter = infrastructurePlanAdapter{
	kind: TerraformPlanKind,
	tool: "terraform",
}
var openTofuPlanReportAdapter registeredAdapter = infrastructurePlanAdapter{
	kind: OpenTofuPlanKind,
	tool: "opentofu",
}
var pyinfraDeployReportAdapter registeredAdapter = pyinfraDeployAdapter{}

var adaptersByMediaType = map[string]registeredAdapter{
	TerraformPlanMediaType: terraformPlanReportAdapter,
	OpenTofuPlanMediaType:  openTofuPlanReportAdapter,
	PyinfraDeployMediaType: pyinfraDeployReportAdapter,
}

var adaptersByKind = map[ReportKind]registeredAdapter{
	TerraformPlanKind: terraformPlanReportAdapter,
	OpenTofuPlanKind:  openTofuPlanReportAdapter,
	PyinfraDeployKind: pyinfraDeployReportAdapter,
}

func AdapterForMediaType(value string) (Adapter, bool) {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return nil, false
	}

	adapter, found := adaptersByMediaType[mediaType]

	return adapter, found
}

func IsRecognizedMediaType(value string) bool {
	_, found := AdapterForMediaType(value)

	return found
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

func DecodeProjection(kind ReportKind, source io.Reader) (Report, error) {
	if source == nil {
		return nil, errors.New("run report projection is required")
	}

	contents, err := io.ReadAll(io.LimitReader(source, MaximumProjectionSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrProjectionRead, err)
	}
	if int64(len(contents)) > MaximumProjectionSizeBytes {
		return nil, fmt.Errorf(
			"%w: projection must not exceed %d bytes",
			ErrProjectionTooLarge,
			MaximumProjectionSizeBytes,
		)
	}

	adapter, found := adaptersByKind[kind]
	if !found {
		return nil, fmt.Errorf("run report kind %q is not supported", kind)
	}
	report := adapter.newReport()

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

	if _, err := Encode(report); err != nil {
		return nil, err
	}

	return report, nil
}

func (e ReportEnvelope) Validate() error {
	adapter, found := adaptersByKind[e.Kind]
	if !found {
		return fmt.Errorf("run report kind %q is not supported", e.Kind)
	}

	return adapter.validateEnvelope(e)
}
