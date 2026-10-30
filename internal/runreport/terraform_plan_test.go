package runreport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestDecodeTerraformPlanFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		check func(*testing.T, TerraformPlanReport, []byte)
	}{
		{
			name: "empty_plan",
			check: func(t *testing.T, report TerraformPlanReport, _ []byte) {
				t.Helper()
				if report.ToolVersion != "1.14.2" || report.Summary.ResourceChanges.Total != 0 ||
					report.Summary.DriftedResources != 0 || report.Summary.Checks.Total != 0 ||
					report.Summary.OutputChanges.Total != 0 {
					t.Errorf("report = %#v", report)
				}
				if report.Summary.Applyable == nil || *report.Summary.Applyable ||
					report.Summary.Complete == nil || !*report.Summary.Complete ||
					report.Summary.Errored == nil || *report.Summary.Errored {
					t.Errorf("plan flags = %#v", report.Summary)
				}
			},
		},
		{
			name: "actions_plan",
			check: func(t *testing.T, report TerraformPlanReport, encoded []byte) {
				t.Helper()
				wantCounts := TerraformActionCounts{
					Total: 7, NoOp: 1, Create: 1, Read: 1, Update: 1, Delete: 1, Replace: 2,
				}
				if report.Summary.ResourceChanges != wantCounts || report.Summary.DriftedResources != 1 {
					t.Errorf("summary = %#v", report.Summary)
				}
				wantOutputs := TerraformActionCounts{Total: 4, NoOp: 1, Create: 1, Update: 1, Delete: 1}
				if report.Summary.OutputChanges != wantOutputs {
					t.Errorf("output counts = %#v", report.Summary.OutputChanges)
				}
				wantChecks := TerraformCheckCounts{Total: 4, Pass: 1, Fail: 1, Error: 1, Unknown: 1}
				if report.Summary.Checks != wantChecks {
					t.Errorf("check counts = %#v", report.Summary.Checks)
				}
				addresses := make([]string, 0, len(report.ResourceChanges))
				orders := map[string]TerraformReplacementOrder{}
				for _, change := range report.ResourceChanges {
					addresses = append(addresses, change.Address)
					if change.ReplacementOrder != "" {
						orders[change.Address] = change.ReplacementOrder
					}
				}
				if !slices.IsSorted(addresses) {
					t.Errorf("resource addresses are not sorted: %q", addresses)
				}
				if orders["aws_instance.recreate_after_destroy"] != TerraformReplacementDestroyBeforeCreate ||
					orders["aws_instance.create_before_destroy"] != TerraformReplacementCreateBeforeDestroy {
					t.Errorf("replacement orders = %#v", orders)
				}
				oldIndex := slices.IndexFunc(report.ResourceChanges, func(change TerraformResourceChange) bool {
					return change.Address == "aws_instance.old"
				})
				if oldIndex < 0 || report.ResourceChanges[oldIndex].ReplacedObjectKey != "deadbeef" {
					t.Errorf("replaced resource object = %#v", report.ResourceChanges)
				}
				for _, secret := range []string{
					"do-not-project", "still-do-not-project", "sensitive.example.com",
					"secret.example.com", "do-not-project-output", "do-not-project-check-message",
				} {
					if bytes.Contains(encoded, []byte(secret)) {
						t.Errorf("encoded projection retained source value %q", secret)
					}
				}
			},
		},
		{
			name: "compatible_unknown_fields",
			check: func(t *testing.T, report TerraformPlanReport, encoded []byte) {
				t.Helper()
				if report.Summary.ResourceChanges.Create != 1 || bytes.Contains(encoded, []byte("future_")) ||
					bytes.Contains(encoded, []byte("ignored")) {
					t.Errorf("report = %#v, encoded = %s", report, encoded)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			contents, err := os.ReadFile("testdata/" + test.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			report, err := DecodeTerraformPlan(bytes.NewReader(contents))
			if err != nil {
				t.Fatalf("DecodeTerraformPlan() error = %v", err)
			}
			encoded, err := Encode(report)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			test.check(t, report, encoded)
		})
	}
}

func TestDecodeTerraformPlanRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	validResourcePrefix := `"address":"example.main","mode":"managed","type":"example",` +
		`"name":"main","provider_name":"registry.example.com/example/provider",`
	tests := []struct {
		name      string
		document  string
		wantError string
	}{
		{name: "malformed", document: `{`, wantError: "decode Terraform plan source"},
		{name: "trailing object", document: `{"format_version":"1.0","applyable":true} {}`, wantError: "unexpected data"},
		{name: "missing format version", document: `{"applyable":true}`, wantError: "format_version is required"},
		{name: "invalid format version", document: `{"format_version":"v1","applyable":true}`, wantError: "numeric major"},
		{name: "unsupported format version", document: `{"format_version":"2.0","applyable":true}`, wantError: "major 2"},
		{name: "state representation", document: `{"format_version":"1.0","values":{}}`, wantError: "does not contain a plan"},
		{
			name:      "unknown action combination",
			document:  `{"format_version":"1.0","resource_changes":[{` + validResourcePrefix + `"change":{"actions":["move"]}}]}`,
			wantError: "action combination is not supported",
		},
		{
			name:      "missing resource address",
			document:  `{"format_version":"1.0","resource_changes":[{"mode":"managed","type":"example","name":"main","provider_name":"registry.example.com/example/provider","change":{"actions":["create"]}}]}`,
			wantError: "address is required",
		},
		{
			name:      "invalid check status",
			document:  `{"format_version":"1.0","checks":[{"status":"pending"}]}`,
			wantError: "not a supported Terraform check status",
		},
		{
			name:      "invalid output action",
			document:  `{"format_version":"1.0","output_changes":{"endpoint":{"actions":["read"]}}}`,
			wantError: `output_changes["endpoint"].actions`,
		},
		{
			name: "retained string too long",
			document: `{"format_version":"1.0","resource_changes":[{` +
				`"address":"` + strings.Repeat("a", maximumResourceAddressLength+1) + `",` +
				`"mode":"managed","type":"example","name":"main",` +
				`"provider_name":"registry.example.com/example/provider","change":{"actions":["create"]}}]}`,
			wantError: "address must not exceed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeTerraformPlan(strings.NewReader(test.document))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DecodeTerraformPlan() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDecodeTerraformPlanEnforcesCollectionBounds(t *testing.T) {
	t.Parallel()

	resource := terraformResourceChangeSource{
		Address: "example.main", Mode: "managed", Type: "example", Name: "main",
		ProviderName: "registry.example.com/example/provider",
		Change:       terraformChangeSource{Actions: []string{"create"}},
	}
	tests := []struct {
		name      string
		document  terraformPlanSource
		wantError string
	}{
		{
			name: "resources",
			document: terraformPlanSource{
				FormatVersion:   "1.0",
				ResourceChanges: slices.Repeat([]terraformResourceChangeSource{resource}, MaximumTerraformResourceEntries+1),
			},
			wantError: "resource_changes and resource_drift",
		},
		{
			name: "outputs",
			document: terraformPlanSource{
				FormatVersion: "1.0",
				OutputChanges: terraformOutputChanges(MaximumTerraformOutputEntries + 1),
			},
			wantError: "output_changes",
		},
		{
			name: "checks",
			document: terraformPlanSource{
				FormatVersion: "1.0",
				Checks: []terraformCheckSource{{
					Status: "pass",
					Instances: slices.Repeat(
						[]terraformCheckInstanceSource{{Status: "pass"}},
						MaximumTerraformCheckEntries+1,
					),
				}},
			},
			wantError: "checks must contain",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			contents, err := json.Marshal(test.document)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeTerraformPlan(bytes.NewReader(contents))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DecodeTerraformPlan() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDecodeTerraformPlanEnforcesSourceSize(t *testing.T) {
	t.Parallel()

	reader := io.LimitReader(zeroReader{}, MaximumTerraformPlanSourceSizeBytes+1)
	_, err := DecodeTerraformPlan(reader)
	if !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("DecodeTerraformPlan() error = %v, want source size error", err)
	}
}

func TestEncodeEnforcesProjectionSizeAndValidation(t *testing.T) {
	t.Parallel()

	report, err := DecodeTerraformPlan(strings.NewReader(`{
		"format_version":"1.0",
		"resource_changes":[{
			"address":"example.main",
			"mode":"managed",
			"type":"example",
			"name":"main",
			"provider_name":"registry.example.com/example/provider",
			"change":{"actions":["create"]}
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	invalidSummary := report
	invalidSummary.Summary.ResourceChanges.Create = 0
	if _, err := Encode(invalidSummary); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Encode(invalid summary) error = %v", err)
	}

	oversized := TerraformPlanReport{
		ReportEnvelope:  report.ReportEnvelope,
		ResourceChanges: make([]TerraformResourceChange, 10_000),
	}
	for index := range oversized.ResourceChanges {
		oversized.ResourceChanges[index] = TerraformResourceChange{
			Address:      fmt.Sprintf("%01990d", index),
			Mode:         "managed",
			Type:         "example",
			Name:         "main",
			ProviderName: "registry.example.com/example/provider",
			Action:       TerraformActionCreate,
		}
		oversized.Summary.ResourceChanges.add(TerraformActionCreate)
	}
	if _, err := Encode(oversized); !errors.Is(err, ErrProjectionTooLarge) {
		t.Fatalf("Encode(oversized) error = %v, want projection size error", err)
	}
}

func TestDecodeProjectionRequiresCanonicalValidatedContent(t *testing.T) {
	t.Parallel()

	report, err := DecodeTerraformPlan(strings.NewReader(`{
		"format_version":"1.0",
		"terraform_version":"1.14.3",
		"resource_changes":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	projection, err := Encode(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeProjection(TerraformPlanKind, bytes.NewReader(projection))
	if err != nil {
		t.Fatalf("DecodeProjection() error = %v", err)
	}
	typed, ok := decoded.(*TerraformPlanReport)
	if !ok || typed.ToolVersion != "1.14.3" {
		t.Fatalf("DecodeProjection() = %#v", decoded)
	}

	withUnknownField := append(bytes.TrimSuffix(projection, []byte("}")), []byte(`,"unknown":true}`)...)
	if _, err := DecodeProjection(TerraformPlanKind, bytes.NewReader(withUnknownField)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("DecodeProjection(unknown field) error = %v", err)
	}
	if _, err := DecodeProjection("unknown", bytes.NewReader(projection)); err == nil {
		t.Fatal("DecodeProjection(unknown) error = nil")
	}
}

func TestReportAdapterRegistryUsesCanonicalMediaType(t *testing.T) {
	t.Parallel()

	adapter, found := AdapterForMediaType(TerraformPlanMediaType + "; charset=utf-8")
	if !found || adapter.Kind() != TerraformPlanKind ||
		adapter.MaximumSourceSizeBytes() != MaximumTerraformPlanSourceSizeBytes {
		t.Fatalf("AdapterForMediaType() = %#v, %t", adapter, found)
	}
	if IsRecognizedMediaType("application/json") {
		t.Fatal("generic JSON was recognized as a Run report")
	}
}

func terraformOutputChanges(count int) map[string]terraformChangeSource {
	result := make(map[string]terraformChangeSource, count)
	for index := range count {
		result[fmt.Sprintf("output-%d", index)] = terraformChangeSource{Actions: []string{"create"}}
	}

	return result
}

type zeroReader struct{}

func (zeroReader) Read(contents []byte) (int, error) {
	for index := range contents {
		contents[index] = 0
	}

	return len(contents), nil
}
