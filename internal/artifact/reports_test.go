package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/runreport"
)

func TestServiceProjectsRunReportArtifactIdempotently(t *testing.T) {
	t.Parallel()

	source := []byte(`{
		"format_version":"1.0",
		"terraform_version":"1.14.2",
		"applyable":true,
		"resource_changes":[{
			"address":"example.main",
			"mode":"managed",
			"type":"example",
			"name":"main",
			"provider_name":"registry.example.com/example/provider",
			"change":{"actions":["create"],"after":{"password":"do-not-project"}}
		}]
	}`)
	request := reportUploadRequest(source)
	metadata := reportTestMetadata()
	contents := &serviceContentStore{}
	observer := &recordingReportIngestionObserver{}
	service := newTestService(metadata, contents)
	service.reportObserver = observer
	service.now = func() time.Time {
		return time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	}

	if err := service.Upload(context.Background(), request); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if contents.puts != 2 || metadata.record == nil || len(metadata.reports) != 1 {
		t.Fatalf("report upload = puts %d, Artifact %#v, reports %#v", contents.puts, metadata.record, metadata.reports)
	}
	report := metadata.reports[metadata.record.ID]
	summary, ok := report.Summary.(*runreport.TerraformPlanSummary)
	if !ok || summary.ResourceChanges.Create != 1 {
		t.Fatalf("report summary = %#v", report.Summary)
	}
	projection := contents.objects[report.ProjectionObjectKey]
	if projection == "" || strings.Contains(projection, "do-not-project") {
		t.Errorf("stored projection = %q", projection)
	}

	records, err := service.List(context.Background(), request.RunID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].Report == nil || records[0].Report.ArtifactID != records[0].ID {
		t.Fatalf("List() = %#v", records)
	}

	request.Body = bytes.NewReader(source)
	if err := service.Upload(context.Background(), request); err != nil {
		t.Fatalf("repeated Upload() error = %v", err)
	}
	if contents.puts != 2 {
		t.Errorf("content puts after retry = %d, want 2", contents.puts)
	}

	request.SizeBytes++
	request.Body = strings.NewReader("different")
	if err := service.Upload(context.Background(), request); !errors.Is(err, ErrChunkConflict) {
		t.Fatalf("conflicting Upload() error = %v", err)
	}
	wantOutcomes := []ReportIngestionOutcome{
		ReportIngestionSucceeded,
		ReportIngestionSucceeded,
		ReportIngestionRejected,
	}
	if !slices.Equal(observer.outcomes, wantOutcomes) {
		t.Errorf("report ingestion outcomes = %v, want %v", observer.outcomes, wantOutcomes)
	}
}

func TestServiceProjectsPyinfraDeployReport(t *testing.T) {
	t.Parallel()

	source := []byte(`{
		"plan":[{
			"op_hash":"package",
			"names":["server.package"],
			"hosts_with_change":["web-1"],
			"hosts_with_conditional_change":[]
		}],
		"results":{
			"operations":[{
				"op_hash":"package",
				"hosts":1,
				"success":["web-1"],
				"error":[],
				"no_change":[]
			}],
			"totals":{"hosts":1,"success":1,"error":0,"no_change":0},
			"failed_hosts":[]
		}
	}`)
	request := reportUploadRequest(source)
	metadata := reportTestMetadata()
	metadata.context.Artifacts[request.Name] = Declaration{
		MediaType: runreport.PyinfraDeployMediaType,
		Sensitive: true,
	}
	contents := &serviceContentStore{}
	service := newTestService(metadata, contents)

	if err := service.Upload(context.Background(), request); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if metadata.record == nil {
		t.Fatal("Upload() did not persist Artifact metadata")
	}
	reportMetadata := metadata.reports[metadata.record.ID]
	if reportMetadata.Kind != runreport.PyinfraDeployKind || reportMetadata.Mode != runreport.ApplyMode {
		t.Fatalf("stored report metadata = %#v", reportMetadata)
	}
	content, err := service.OpenReport(context.Background(), metadata.record.ID)
	if err != nil {
		t.Fatalf("OpenReport() error = %v", err)
	}
	report, ok := content.Report.(*runreport.PyinfraDeployReport)
	if !ok || report.Summary.Results == nil || report.Summary.Results.ChangedHostOperations != 1 ||
		len(report.Operations) != 1 || report.Operations[0].Names[0] != "server.package" {
		t.Fatalf("OpenReport() report = %#v", content.Report)
	}
}

func TestServiceLeavesReportObjectsForReconciliationAfterFailure(t *testing.T) {
	t.Parallel()

	validSource := []byte(`{"format_version":"1.0","applyable":false,"resource_changes":[]}`)
	tests := []struct {
		name        string
		source      []byte
		configure   func(*serviceMetadataStore, *serviceContentStore)
		wantPuts    int
		wantOutcome ReportIngestionOutcome
		wantInvalid bool
	}{
		{
			name:        "malformed source",
			source:      []byte(`{"format_version":`),
			wantPuts:    1,
			wantOutcome: ReportIngestionRejected,
			wantInvalid: true,
		},
		{
			name:   "projection storage failure",
			source: validSource,
			configure: func(_ *serviceMetadataStore, contents *serviceContentStore) {
				contents.err = errors.New("object store unavailable")
				contents.failPut = 2
			},
			wantPuts:    2,
			wantOutcome: ReportIngestionFailed,
		},
		{
			name:   "metadata transaction failure",
			source: validSource,
			configure: func(metadata *serviceMetadataStore, _ *serviceContentStore) {
				metadata.reportErr = errors.New("database unavailable")
			},
			wantPuts:    2,
			wantOutcome: ReportIngestionFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			metadata := reportTestMetadata()
			contents := &serviceContentStore{}
			if test.configure != nil {
				test.configure(metadata, contents)
			}
			observer := &recordingReportIngestionObserver{}
			service := newTestService(metadata, contents)
			service.reportObserver = observer
			err := service.Upload(context.Background(), reportUploadRequest(test.source))
			if err == nil || test.wantInvalid != errors.Is(err, ErrInvalid) {
				t.Fatalf("Upload() error = %v, want invalid=%t", err, test.wantInvalid)
			}
			if metadata.record != nil || len(metadata.reports) != 0 {
				t.Errorf("failed upload persisted metadata: Artifact %#v, reports %#v", metadata.record, metadata.reports)
			}
			if contents.puts != test.wantPuts || len(contents.objects) == 0 {
				t.Errorf("failed upload objects = puts %d, objects %#v", contents.puts, contents.objects)
			}
			if len(observer.outcomes) != 1 || observer.outcomes[0] != test.wantOutcome {
				t.Errorf("report ingestion outcomes = %v, want %q", observer.outcomes, test.wantOutcome)
			}
		})
	}
}

func reportTestMetadata() *serviceMetadataStore {
	return &serviceMetadataStore{
		runCluster: "colo",
		context: RunContext{Artifacts: map[string]Declaration{
			"plan-report": {
				MediaType: runreport.TerraformPlanMediaType,
				Sensitive: true,
			},
		}},
	}
}

func reportUploadRequest(source []byte) UploadRequest {
	digest := sha256.Sum256(source)

	return UploadRequest{
		RunID:     uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1510"),
		Name:      "plan-report",
		Body:      bytes.NewReader(source),
		SizeBytes: int64(len(source)),
		SHA256:    hex.EncodeToString(digest[:]),
	}
}

type recordingReportIngestionObserver struct {
	kinds    []runreport.ReportKind
	outcomes []ReportIngestionOutcome
}

func (o *recordingReportIngestionObserver) ObserveReportIngestion(
	_ context.Context,
	kind runreport.ReportKind,
	outcome ReportIngestionOutcome,
	_ time.Duration,
) {
	o.kinds = append(o.kinds, kind)
	o.outcomes = append(o.outcomes, outcome)
}
