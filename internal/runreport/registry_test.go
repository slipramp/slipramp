package runreport

import (
	"bytes"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/runreport/ansible"
	ansiblesource "github.com/slipramp/slipramp/internal/runreport/ansible/source"
	"github.com/slipramp/slipramp/internal/runreport/infrastructure"
	"github.com/slipramp/slipramp/internal/runreport/model"
	"github.com/slipramp/slipramp/internal/runreport/pyinfra"
	"github.com/slipramp/slipramp/internal/runreport/steps"
)

func TestReportAdapterRegistryUsesCanonicalMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mediaType string
		kind      model.Kind
		maximum   int64
	}{
		{
			mediaType: infrastructure.TerraformPlanMediaType,
			kind:      infrastructure.TerraformPlanKind,
			maximum:   infrastructure.MaximumSourceSizeBytes,
		},
		{
			mediaType: infrastructure.OpenTofuPlanMediaType,
			kind:      infrastructure.OpenTofuPlanKind,
			maximum:   infrastructure.MaximumSourceSizeBytes,
		},
		{mediaType: pyinfra.DeployMediaType, kind: pyinfra.DeployKind, maximum: pyinfra.MaximumSourceSizeBytes},
		{mediaType: ansible.PlaybookMediaType, kind: ansible.PlaybookKind, maximum: ansiblesource.MaximumSourceSize},
		{mediaType: steps.ReportMediaType, kind: steps.ReportKind, maximum: steps.MaximumSourceSizeBytes},
	}
	for _, test := range tests {
		adapter, found := AdapterForMediaType(test.mediaType + "; charset=utf-8")
		if !found || adapter.Kind() != test.kind ||
			adapter.MaximumSourceSizeBytes() != test.maximum {
			t.Fatalf("AdapterForMediaType(%q) = %#v, %t", test.mediaType, adapter, found)
		}
	}
	if IsRecognizedMediaType("application/json") {
		t.Fatal("generic JSON was recognized as a Run report")
	}
}

func TestDecodeProjectionRequiresCanonicalValidatedContent(t *testing.T) {
	t.Parallel()

	report, err := infrastructure.DecodeTerraformPlan(strings.NewReader(`{
		"format_version":"1.0",
		"terraform_version":"1.14.3",
		"resource_changes":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	projection, err := model.Encode(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeProjection(infrastructure.TerraformPlanKind, bytes.NewReader(projection))
	if err != nil {
		t.Fatalf("DecodeProjection() error = %v", err)
	}
	typed, ok := decoded.(*infrastructure.InfrastructurePlanReport)
	if !ok || typed.ToolVersion != "1.14.3" {
		t.Fatalf("DecodeProjection() = %#v", decoded)
	}

	withUnknownField := append(bytes.TrimSuffix(projection, []byte("}")), []byte(`,"unknown":true}`)...)
	if _, err := DecodeProjection(
		infrastructure.TerraformPlanKind,
		bytes.NewReader(withUnknownField),
	); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("DecodeProjection(unknown field) error = %v", err)
	}
	if _, err := DecodeProjection("unknown", bytes.NewReader(projection)); err == nil {
		t.Fatal("DecodeProjection(unknown) error = nil")
	}
}
