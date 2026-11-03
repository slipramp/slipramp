package runreport

import "testing"

func TestReportAdapterRegistryUsesCanonicalMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mediaType string
		kind      ReportKind
		maximum   int64
	}{
		{mediaType: TerraformPlanMediaType, kind: TerraformPlanKind, maximum: MaximumInfrastructurePlanSourceSizeBytes},
		{mediaType: OpenTofuPlanMediaType, kind: OpenTofuPlanKind, maximum: MaximumInfrastructurePlanSourceSizeBytes},
		{mediaType: PyinfraDeployMediaType, kind: PyinfraDeployKind, maximum: MaximumPyinfraDeploySourceSizeBytes},
		{mediaType: AnsiblePlaybookMediaType, kind: AnsiblePlaybookKind, maximum: MaximumAnsiblePlaybookSourceSizeBytes},
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
