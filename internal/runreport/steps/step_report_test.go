package steps

import (
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/runreport/model"
)

func TestDecodeStepReport(t *testing.T) {
	t.Parallel()

	report, err := DecodeStepReport(strings.NewReader(`{
		"schemaVersion":1,
		"tool":"upgrade_k8s.py",
		"title":"Kubernetes upgrade",
		"mode":"apply",
		"outcome":"warning",
		"durationMs":12500,
		"steps":[
			{
				"id":"controlplane100",
				"name":"controlplane100",
				"outcome":"warning",
				"durationMs":12000,
				"steps":[
					{"id":"preflight","name":"Preflight","outcome":"succeeded","durationMs":1500},
					{"id":"postflight","name":"Postflight","outcome":"warning","durationMs":500}
				]
			},
			{"id":"cleanup","name":"Cleanup","outcome":"skipped","durationMs":0}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if report.Tool != "upgrade_k8s.py" || report.Title != "Kubernetes upgrade" ||
		report.Mode != model.ApplyMode || report.Summary.Outcome != OutcomeWarning ||
		report.Summary.DurationMS != 12500 || len(report.Steps) != 2 || len(report.Steps[0].Steps) != 2 {
		t.Fatalf("DecodeStepReport() = %#v", report)
	}
	wantCounts := StepOutcomeCounts{Total: 3, Succeeded: 1, Warning: 1, Skipped: 1}
	if report.Summary.Steps != wantCounts {
		t.Errorf("summary steps = %#v, want %#v", report.Summary.Steps, wantCounts)
	}
	if _, err := model.Encode(report); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
}

func TestDecodeStepReportRejectsInvalidSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		document  string
		wantError string
	}{
		{
			name: "unknown field",
			document: `{"schemaVersion":1,"tool":"script","mode":"apply","outcome":"succeeded",` +
				`"durationMs":1,"steps":[],"extra":true}`,
			wantError: "unknown field",
		},
		{
			name: "duplicate sibling ID",
			document: `{"schemaVersion":1,"tool":"script","mode":"apply","outcome":"succeeded",` +
				`"durationMs":1,"steps":[` +
				`{"id":"same","name":"One","outcome":"succeeded","durationMs":1},` +
				`{"id":"same","name":"Two","outcome":"succeeded","durationMs":1}]}`,
			wantError: "duplicated",
		},
		{
			name: "too deeply nested",
			document: `{"schemaVersion":1,"tool":"script","mode":"apply","outcome":"succeeded",` +
				`"durationMs":1,"steps":[{"id":"one","name":"One","outcome":"succeeded",` +
				`"durationMs":1,"steps":[{"id":"two","name":"Two","outcome":"succeeded",` +
				`"durationMs":1,"steps":[{"id":"three","name":"Three","outcome":"succeeded",` +
				`"durationMs":1}]}]}]}`,
			wantError: "maximum nesting depth",
		},
		{
			name: "negative duration",
			document: `{"schemaVersion":1,"tool":"script","mode":"apply","outcome":"succeeded",` +
				`"durationMs":-1,"steps":[]}`,
			wantError: "must not be negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeStepReport(strings.NewReader(test.document))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DecodeStepReport() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestStepReportValidationRequiresMatchingLeafCounts(t *testing.T) {
	t.Parallel()

	report, err := DecodeStepReport(strings.NewReader(`{
		"schemaVersion":1,
		"tool":"script",
		"mode":"preview",
		"outcome":"succeeded",
		"durationMs":10,
		"steps":[{"id":"check","name":"Check","outcome":"succeeded","durationMs":10}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	report.Summary.Steps.Succeeded = 0
	report.Summary.Steps.Failed = 1
	if err := report.Validate(); err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("Validate() error = %v", err)
	}
}
