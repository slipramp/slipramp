package runreport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestDecodePyinfraDeployFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		check func(*testing.T, PyinfraDeployReport, []byte)
	}{
		{
			name: "pyinfra_preview",
			check: func(t *testing.T, report PyinfraDeployReport, encoded []byte) {
				t.Helper()
				wantPlan := PyinfraPlanSummary{
					TotalOperations:           3,
					ChangedOperations:         2,
					UnchangedOperations:       1,
					AffectedHosts:             2,
					PlannedHostOperations:     2,
					ConditionalHostOperations: 1,
				}
				if report.Mode != PreviewMode || report.Summary.Plan != wantPlan ||
					report.Summary.Results != nil || len(report.FailedHosts) != 0 {
					t.Errorf("report = %#v", report)
				}
				if got := report.Operations[0].HostsWithChanges; !slices.Equal(
					got,
					[]string{"node-01.example.com", "node-02.example.com"},
				) {
					t.Errorf("sorted planned hosts = %q", got)
				}
				if report.Operations[2].Names[0] != "Check node exporter service" {
					t.Errorf("operation order = %#v", report.Operations)
				}
				if bytes.Contains(encoded, []byte("do-not-project")) ||
					bytes.Contains(encoded, []byte("future_field")) {
					t.Errorf("projection retained excluded source data: %s", encoded)
				}
			},
		},
		{
			name: "pyinfra_apply",
			check: func(t *testing.T, report PyinfraDeployReport, encoded []byte) {
				t.Helper()
				wantResults := PyinfraResultSummary{
					HostOperations:          4,
					ChangedHostOperations:   2,
					FailedHostOperations:    1,
					UnchangedHostOperations: 1,
					FailedHosts:             1,
				}
				if report.Mode != ApplyMode || report.Summary.Results == nil ||
					*report.Summary.Results != wantResults {
					t.Errorf("report = %#v", report)
				}
				installResult := report.Operations[0].Result
				if installResult == nil || !slices.Equal(
					installResult.ChangedHosts,
					[]string{"node-01.example.com"},
				) || !slices.Equal(installResult.FailedHosts, []string{"node-02.example.com"}) {
					t.Errorf("joined install result = %#v", installResult)
				}
				if !slices.Equal(report.FailedHosts, []string{"node-02.example.com"}) {
					t.Errorf("failed hosts = %q", report.FailedHosts)
				}
				if bytes.Contains(encoded, []byte("do-not-project")) {
					t.Errorf("projection retained operation arguments: %s", encoded)
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
			report, err := DecodePyinfraDeploy(bytes.NewReader(contents))
			if err != nil {
				t.Fatalf("DecodePyinfraDeploy() error = %v", err)
			}
			encoded, err := Encode(report)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			test.check(t, report, encoded)

			decoded, err := DecodeProjection(PyinfraDeployKind, bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("DecodeProjection() error = %v", err)
			}
			if _, ok := decoded.(*PyinfraDeployReport); !ok {
				t.Fatalf("DecodeProjection() = %T", decoded)
			}
		})
	}
}

func TestDecodePyinfraDeployRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	plan := `{"plan":[{"op_hash":"one","names":["One"],` +
		`"hosts_with_change":[],"hosts_with_conditional_change":[]}],`
	applyResult := `"results":{"operations":[{"op_hash":"one","hosts":1,` +
		`"success":["host"],"error":[],"no_change":[]}],` +
		`"totals":{"hosts":1,"success":1,"error":0,"no_change":0},"failed_hosts":[]}}`
	tests := []struct {
		name      string
		document  string
		wantError string
	}{
		{name: "malformed", document: `{`, wantError: "decode pyinfra deploy source"},
		{name: "trailing object", document: `{"plan":[],"results":null} {}`, wantError: "unexpected data"},
		{name: "missing plan", document: `{"results":null}`, wantError: "plan is required"},
		{name: "missing results", document: `{"plan":[]}`, wantError: "results are required"},
		{
			name: "missing operation names",
			document: `{"plan":[{"op_hash":"one","hosts_with_change":[],` +
				`"hosts_with_conditional_change":[]}],"results":null}`,
			wantError: "names is required",
		},
		{
			name: "duplicate plan hash",
			document: `{"plan":[` +
				`{"op_hash":"one","names":["One"],"hosts_with_change":[],"hosts_with_conditional_change":[]},` +
				`{"op_hash":"one","names":["Two"],"hosts_with_change":[],"hosts_with_conditional_change":[]}` +
				`],"results":null}`,
			wantError: "op_hash is duplicated",
		},
		{
			name: "overlapping planned hosts",
			document: `{"plan":[{"op_hash":"one","names":["One"],` +
				`"hosts_with_change":["host"],"hosts_with_conditional_change":["host"]}],` +
				`"results":null}`,
			wantError: "host lists overlap",
		},
		{
			name:      "missing result operation",
			document:  plan + `"results":{"operations":[],"totals":{},"failed_hosts":[]}}`,
			wantError: "every planned operation",
		},
		{
			name: "unknown result operation",
			document: plan + `"results":{"operations":[{"op_hash":"two","hosts":0,` +
				`"success":[],"error":[],"no_change":[]}],"totals":{},"failed_hosts":[]}}`,
			wantError: "does not match a planned operation",
		},
		{
			name: "mismatched operation hosts",
			document: plan + `"results":{"operations":[{"op_hash":"one","hosts":2,` +
				`"success":["host"],"error":[],"no_change":[]}],` +
				`"totals":{"hosts":1,"success":1,"error":0,"no_change":0},"failed_hosts":[]}}`,
			wantError: ".hosts does not match",
		},
		{
			name: "overlapping result hosts",
			document: plan + `"results":{"operations":[{"op_hash":"one","hosts":2,` +
				`"success":["host"],"error":["host"],"no_change":[]}],` +
				`"totals":{"hosts":2,"success":1,"error":1,"no_change":0},"failed_hosts":["host"]}}`,
			wantError: "outcome lists overlap",
		},
		{
			name:      "mismatched totals",
			document:  plan + strings.Replace(applyResult, `"success":1`, `"success":0`, 1),
			wantError: "totals do not match",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodePyinfraDeploy(strings.NewReader(test.document))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DecodePyinfraDeploy() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDecodePyinfraDeployEnforcesBounds(t *testing.T) {
	t.Parallel()

	operation := pyinfraPlanOperationSource{
		Hash: "one", Names: []string{"One"}, HostsWithChange: []string{},
		HostsWithConditionalChange: []string{},
	}
	tests := []struct {
		name      string
		document  pyinfraDeploySource
		wantError string
	}{
		{
			name: "operations",
			document: pyinfraDeploySource{
				Plan:    slices.Repeat([]pyinfraPlanOperationSource{operation}, MaximumPyinfraOperations+1),
				Results: json.RawMessage("null"),
			},
			wantError: "plan must contain",
		},
		{
			name: "host entries",
			document: pyinfraDeploySource{
				Plan: []pyinfraPlanOperationSource{{
					Hash: "one", Names: []string{"One"},
					HostsWithChange:            slices.Repeat([]string{"host"}, MaximumPyinfraHostEntries+1),
					HostsWithConditionalChange: []string{},
				}},
				Results: json.RawMessage("null"),
			},
			wantError: "hosts_with_change must contain",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			contents, err := json.Marshal(test.document)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodePyinfraDeploy(bytes.NewReader(contents))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DecodePyinfraDeploy() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDecodePyinfraDeployEnforcesSourceSize(t *testing.T) {
	t.Parallel()

	reader := io.LimitReader(zeroReader{}, MaximumPyinfraDeploySourceSizeBytes+1)
	_, err := DecodePyinfraDeploy(reader)
	if !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("DecodePyinfraDeploy() error = %v, want source size error", err)
	}
}

func TestPyinfraReportAdapterRegistry(t *testing.T) {
	t.Parallel()

	adapter, found := AdapterForMediaType(PyinfraDeployMediaType + "; charset=utf-8")
	if !found || adapter.Kind() != PyinfraDeployKind ||
		adapter.MaximumSourceSizeBytes() != MaximumPyinfraDeploySourceSizeBytes {
		t.Fatalf("AdapterForMediaType() = %#v, %t", adapter, found)
	}
}
