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

func TestDecodeAnsiblePlaybookFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		check func(*testing.T, AnsiblePlaybookReport)
	}{
		{
			name: "ansible_success_apply",
			check: func(t *testing.T, report AnsiblePlaybookReport) {
				t.Helper()
				wantResults := AnsibleOutcomeCounts{Total: 6, OK: 2, Changed: 4}
				if report.Mode != ApplyMode || !report.Summary.Complete || report.Summary.Hosts != 2 ||
					report.Summary.Plays != 1 || report.Summary.Tasks != 3 || report.Summary.Results != wantResults {
					t.Errorf("report summary = %#v", report.Summary)
				}
				if report.ToolVersion != "2.21.5" || report.Runner.Version != "2.4.3" ||
					report.Runner.Status != AnsibleRunnerStatusSuccessful || report.Runner.ExitCode != 0 {
					t.Errorf("report versions and Runner = %#v %#v", report.ReportEnvelope, report.Runner)
				}
				if got := report.Summary.HostTotals[0]; got.Host != "node01.example.com" ||
					got.OK != 3 || got.Changed != 3 {
					t.Errorf("node01 totals = %#v", got)
				}
				if got := report.Plays[0].Tasks[0].SourcePath; got != "ansible/site.yml" {
					t.Errorf("normalized source path = %q", got)
				}
				loopResults := report.Plays[0].Tasks[1].Results
				if len(loopResults) != 3 || !loopResults[0].LoopItem || !loopResults[1].LoopItem ||
					loopResults[2].LoopItem {
					t.Errorf("loop results = %#v", loopResults)
				}
				if handler := report.Plays[0].Tasks[2]; handler.Name != "Restart web service" ||
					handler.SourcePath != "ansible/roles/web/handlers/main.yml" {
					t.Errorf("handler task = %#v", handler)
				}
			},
		},
		{
			name: "ansible_preview",
			check: func(t *testing.T, report AnsiblePlaybookReport) {
				t.Helper()
				wantResults := AnsibleOutcomeCounts{Total: 1, Changed: 1}
				if report.Mode != PreviewMode || !report.Summary.Complete || report.Summary.Results != wantResults ||
					report.Plays[0].Tasks[0].SourcePath != "site.yml" {
					t.Errorf("preview report = %#v", report)
				}
			},
		},
		{
			name: "ansible_failure_unreachable",
			check: func(t *testing.T, report AnsiblePlaybookReport) {
				t.Helper()
				wantResults := AnsibleOutcomeCounts{Total: 5, Changed: 1, Failed: 2, Unreachable: 1, Skipped: 1}
				if !report.Summary.Complete || report.Summary.Results != wantResults ||
					report.Runner.Status != AnsibleRunnerStatusFailed || report.Runner.ExitCode != 2 {
					t.Errorf("failure report = %#v", report)
				}
				if totals := report.Summary.HostTotals[0]; totals.Host != "node01.example.com" ||
					totals.Rescued != 1 || totals.Ignored != 1 {
					t.Errorf("failure host totals = %#v", totals)
				}
				ignored := report.Plays[0].Tasks[2].Results[0]
				if ignored.Outcome != AnsibleOutcomeFailed || !ignored.IgnoredFailure {
					t.Errorf("ignored result = %#v", ignored)
				}
				unreachable := report.Plays[0].Tasks[3].Results[0]
				if unreachable.Outcome != AnsibleOutcomeUnreachable {
					t.Errorf("unreachable result = %#v", unreachable)
				}
				skipped := report.Plays[0].Tasks[4].Results[0]
				if skipped.Outcome != AnsibleOutcomeSkipped {
					t.Errorf("skipped result = %#v", skipped)
				}
			},
		},
		{
			name: "ansible_no_log",
			check: func(t *testing.T, report AnsiblePlaybookReport) {
				t.Helper()
				task := report.Plays[0].Tasks[0]
				if !task.Redacted || task.Name != ansibleRedactedName || task.Action != ansibleRedactedName ||
					task.SourcePath != "" {
					t.Errorf("no_log task = %#v", task)
				}
			},
		},
		{
			name: "ansible_partial_canceled",
			check: func(t *testing.T, report AnsiblePlaybookReport) {
				t.Helper()
				wantTotals := AnsibleHostTotals{Host: "node01.example.com", OK: 1, Changed: 1}
				if report.Summary.Complete || report.Runner.Status != AnsibleRunnerStatusCanceled ||
					report.Runner.ExitCode != 143 || report.Summary.Hosts != 1 ||
					report.Summary.HostTotals[0] != wantTotals {
					t.Errorf("partial report = %#v", report)
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
			report, err := DecodeAnsiblePlaybook(bytes.NewReader(contents))
			if err != nil {
				t.Fatalf("DecodeAnsiblePlaybook() error = %v", err)
			}
			test.check(t, report)

			encoded, err := Encode(report)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if bytes.Contains(encoded, []byte("source-only")) {
				t.Errorf("projection retained excluded source data: %s", encoded)
			}

			var decoded AnsiblePlaybookReport
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if err := decoded.Validate(); err != nil {
				t.Fatalf("round-trip report validation error = %v", err)
			}

			projection, err := DecodeProjection(AnsiblePlaybookKind, bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("DecodeProjection() error = %v", err)
			}
			if _, ok := projection.(*AnsiblePlaybookReport); !ok {
				t.Fatalf("DecodeProjection() = %T", projection)
			}
		})
	}
}

func TestDecodeAnsiblePlaybookRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	statsWithoutIgnored := `{
		"changed":{},"dark":{},"failures":{},"ok":{},"processed":{},"rescued":{},"skipped":{}
	}`
	tests := []struct {
		name      string
		document  string
		wantError string
	}{
		{name: "malformed", document: `{`, wantError: "decode Ansible playbook source"},
		{name: "trailing object", document: validAnsibleSource(`[]`) + ` {}`, wantError: "unexpected data"},
		{
			name:      "missing versions",
			document:  `{"schemaVersion":1,"mode":"apply","runner":{"status":"successful","exitCode":0},"events":[]}`,
			wantError: "versions are required",
		},
		{
			name:      "missing runner",
			document:  `{"schemaVersion":1,"mode":"apply","versions":{},"events":[]}`,
			wantError: "runner status and exitCode are required",
		},
		{
			name:      "missing exit code",
			document:  `{"schemaVersion":1,"mode":"apply","versions":{},"runner":{"status":"successful"},"events":[]}`,
			wantError: "runner status and exitCode are required",
		},
		{
			name:      "missing events",
			document:  `{"schemaVersion":1,"mode":"apply","versions":{},"runner":{"status":"successful","exitCode":0}}`,
			wantError: "events are required",
		},
		{name: "schema version", document: strings.Replace(validAnsibleSource(`[]`), `"schemaVersion":1`, `"schemaVersion":2`, 1), wantError: "not supported"},
		{name: "mode", document: strings.Replace(validAnsibleSource(`[]`), `"mode":"apply"`, `"mode":"plan"`, 1), wantError: "mode must be"},
		{name: "runner status", document: strings.Replace(validAnsibleSource(`[]`), `"status":"successful"`, `"status":"unknown"`, 1), wantError: "status is not supported"},
		{name: "runner exit code", document: strings.Replace(validAnsibleSource(`[]`), `"exitCode":0`, `"exitCode":256`, 1), wantError: "between 0 and 255"},
		{
			name: "unordered counters",
			document: validAnsibleSource(`[
				{"counter":2,"uuid":"event-2","event":"future"},
				{"counter":1,"uuid":"event-1","event":"future"}
			]`),
			wantError: "strictly increasing counters",
		},
		{
			name:      "missing recognized event data",
			document:  validAnsibleSource(`[{"counter":1,"uuid":"event-1","event":"playbook_on_play_start"}]`),
			wantError: "event_data is required",
		},
		{
			name: "missing task identity",
			document: validAnsibleSource(`[{"counter":1,"uuid":"event-1","event":"runner_on_ok","event_data":{
				"play":"Play","play_uuid":"play","task":"Task","task_action":"copy","host":"host","res":{}
			}}]`),
			wantError: "task identity is required",
		},
		{
			name: "malformed result",
			document: validAnsibleSource(`[{"counter":1,"uuid":"event-1","event":"runner_on_ok","event_data":{
				"play":"Play","play_uuid":"play","task":"Task","task_uuid":"task","task_action":"copy","host":"host","res":"invalid"
			}}]`),
			wantError: "decode events[0].event_data.res",
		},
		{
			name: "unsafe host",
			document: validAnsibleSource(`[{"counter":1,"uuid":"event-1","event":"runner_on_ok","event_data":{
				"play":"Play","play_uuid":"play","task":"Task","task_uuid":"task","task_action":"copy","host":"bad\nhost","res":{}
			}}]`),
			wantError: "unsafe control characters",
		},
		{
			name: "unsafe source path",
			document: validAnsibleSource(`[{"counter":1,"uuid":"event-1","event":"playbook_on_task_start","event_data":{
				"play":"Play","play_uuid":"play","task":"Task","task_uuid":"task","task_action":"copy","task_path":"../secret.yml"
			}}]`),
			wantError: "repository-relative",
		},
		{
			name:      "missing stats map",
			document:  validAnsibleSource(fmt.Sprintf(`[{"counter":1,"uuid":"stats","event":"playbook_on_stats","event_data":%s}]`, statsWithoutIgnored)),
			wantError: "event_data.ignored is required",
		},
		{
			name: "multiple stats",
			document: validAnsibleSource(`[
				{"counter":1,"uuid":"stats-1","event":"playbook_on_stats","event_data":{"changed":{},"dark":{},"failures":{},"ignored":{},"ok":{},"processed":{},"rescued":{},"skipped":{}}},
				{"counter":2,"uuid":"stats-2","event":"playbook_on_stats","event_data":{"changed":{},"dark":{},"failures":{},"ignored":{},"ok":{},"processed":{},"rescued":{},"skipped":{}}}
			]`),
			wantError: "multiple playbook_on_stats",
		},
		{
			name: "result host absent from stats",
			document: validAnsibleSource(`[
				{"counter":1,"uuid":"result","event":"runner_on_ok","event_data":{"play":"Play","play_uuid":"play","task":"Task","task_uuid":"task","task_action":"copy","host":"host","res":{}}},
				{"counter":2,"uuid":"stats","event":"playbook_on_stats","event_data":{"changed":{},"dark":{},"failures":{},"ignored":{},"ok":{},"processed":{},"rescued":{},"skipped":{}}}
			]`),
			wantError: "missing from final playbook stats",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeAnsiblePlaybook(strings.NewReader(test.document))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DecodeAnsiblePlaybook() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDecodeAnsiblePlaybookEnforcesBounds(t *testing.T) {
	t.Parallel()

	exitCode := 0
	document := ansiblePlaybookSource{
		SchemaVersion: AnsiblePlaybookSchemaVersion,
		Mode:          ApplyMode,
		Versions:      &ansibleVersionsSource{},
		Runner: &ansibleRunnerSource{
			Status:   AnsibleRunnerStatusSuccessful,
			ExitCode: &exitCode,
		},
		Events: slices.Repeat(
			[]json.RawMessage{json.RawMessage(`{"counter":1,"uuid":"event","event":"future"}`)},
			MaximumAnsiblePlaybookEvents+1,
		),
	}
	contents, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAnsiblePlaybook(bytes.NewReader(contents)); err == nil ||
		!strings.Contains(err.Error(), "events must contain") {
		t.Fatalf("DecodeAnsiblePlaybook() event bound error = %v", err)
	}

	report := validAnsibleProjection()
	report.Plays = make([]AnsiblePlay, MaximumAnsiblePlaybookPlays+1)
	if err := report.Validate(); err == nil || !strings.Contains(err.Error(), "plays must contain") {
		t.Fatalf("Validate() play bound error = %v", err)
	}

	report = validAnsibleProjection()
	report.Plays[0].Tasks = make([]AnsibleTask, MaximumAnsiblePlaybookTasks+1)
	if err := report.Validate(); err == nil || !strings.Contains(err.Error(), "at most 20000 tasks") {
		t.Fatalf("Validate() task bound error = %v", err)
	}

	report = validAnsibleProjection()
	report.Plays[0].Tasks = []AnsibleTask{{
		Order:   2,
		ID:      "task",
		Name:    "Task",
		Action:  "copy",
		Results: make([]AnsibleTaskResult, MaximumAnsiblePlaybookResults+1),
	}}
	if err := report.Validate(); err == nil || !strings.Contains(err.Error(), "host-task results") {
		t.Fatalf("Validate() result bound error = %v", err)
	}

	report = validAnsibleProjection()
	report.Summary.Hosts = MaximumAnsiblePlaybookHosts + 1
	report.Summary.HostTotals = make([]AnsibleHostTotals, MaximumAnsiblePlaybookHosts+1)
	if err := report.Validate(); err == nil || !strings.Contains(err.Error(), "outside report bounds") {
		t.Fatalf("Validate() host bound error = %v", err)
	}
}

func TestDecodeAnsiblePlaybookEnforcesSourceSize(t *testing.T) {
	t.Parallel()

	reader := io.LimitReader(zeroReader{}, MaximumAnsiblePlaybookSourceSizeBytes+1)
	_, err := DecodeAnsiblePlaybook(reader)
	if !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("DecodeAnsiblePlaybook() error = %v, want source size error", err)
	}
}

func TestAnsiblePlaybookAdapter(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/ansible_preview.json")
	if err != nil {
		t.Fatal(err)
	}
	adapter := ansiblePlaybookAdapter{}
	report, err := adapter.Decode(bytes.NewReader(contents))
	if err != nil {
		t.Fatalf("adapter.Decode() error = %v", err)
	}
	if adapter.Kind() != AnsiblePlaybookKind ||
		adapter.MaximumSourceSizeBytes() != MaximumAnsiblePlaybookSourceSizeBytes {
		t.Errorf("adapter metadata = %q %d", adapter.Kind(), adapter.MaximumSourceSizeBytes())
	}
	if _, ok := report.(*AnsiblePlaybookReport); !ok {
		t.Fatalf("adapter.Decode() = %T", report)
	}
}

func TestAnsiblePlaybookAdapterRegistry(t *testing.T) {
	t.Parallel()

	adapter, found := AdapterForMediaType(AnsiblePlaybookMediaType + "; charset=utf-8")
	if !found || adapter.Kind() != AnsiblePlaybookKind ||
		adapter.MaximumSourceSizeBytes() != MaximumAnsiblePlaybookSourceSizeBytes {
		t.Fatalf("AdapterForMediaType() = %#v, %t", adapter, found)
	}
}

func validAnsibleSource(events string) string {
	return `{
		"schemaVersion":1,
		"mode":"apply",
		"versions":{"ansibleCore":"2.21.5","ansibleRunner":"2.4.3"},
		"runner":{"status":"successful","exitCode":0},
		"events":` + events + `
	}`
}

func validAnsibleProjection() AnsiblePlaybookReport {
	envelope := ReportEnvelope{
		Kind:           AnsiblePlaybookKind,
		SchemaVersion:  AnsiblePlaybookSchemaVersion,
		AdapterVersion: AnsiblePlaybookAdapterVersion,
		Tool:           "ansible",
		ToolVersion:    "2.21.5",
		Mode:           ApplyMode,
	}

	return AnsiblePlaybookReport{
		ReportEnvelope: envelope,
		Runner: AnsibleRunnerSummary{
			Version:  "2.4.3",
			Status:   AnsibleRunnerStatusSuccessful,
			ExitCode: 0,
		},
		Summary: AnsiblePlaybookSummary{
			Plays:      1,
			HostTotals: []AnsibleHostTotals{},
		},
		Plays: []AnsiblePlay{{
			Order: 1,
			ID:    "play",
			Name:  "Play",
			Tasks: []AnsibleTask{},
		}},
	}
}
