package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/runreport"
)

func TestServiceOpensVerifiedRunReportProjection(t *testing.T) {
	t.Parallel()

	service, record, metadata := reportReadService(t)
	content, err := service.OpenReport(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("OpenReport() error = %v", err)
	}
	if content.Artifact.ID != record.ID || content.Metadata.ArtifactID != record.ID {
		t.Fatalf("OpenReport() = %#v", content)
	}
	report, ok := content.Report.(*runreport.TerraformPlanReport)
	if !ok || report.Summary.ResourceChanges.Create != 1 || report.ResourceChanges[0].Address != "example.main" {
		t.Fatalf("OpenReport() report = %#v", content.Report)
	}
	if report.Metadata() != metadata.ReportEnvelope {
		t.Errorf("OpenReport() envelope = %#v, want %#v", report.Metadata(), metadata.ReportEnvelope)
	}
}

func TestServiceRejectsUnavailableOrInvalidRunReportProjection(t *testing.T) {
	t.Parallel()

	objectFailure := errors.New("object store unavailable")
	tests := []struct {
		name       string
		configure  func(*serviceMetadataStore, *serviceContentStore, Artifact, runreport.ArtifactReport)
		wantError  error
		wantPhrase string
	}{
		{
			name: "missing report metadata",
			configure: func(metadata *serviceMetadataStore, _ *serviceContentStore, _ Artifact, _ runreport.ArtifactReport) {
				metadata.reports = nil
			},
			wantError: ErrNotFound,
		},
		{
			name: "expired Artifact",
			configure: func(metadata *serviceMetadataStore, _ *serviceContentStore, record Artifact, _ runreport.ArtifactReport) {
				record.RetentionState = RetentionExpired
				metadata.record = &record
			},
			wantError: ErrNotFound,
		},
		{
			name: "projection size mismatch",
			configure: func(metadata *serviceMetadataStore, _ *serviceContentStore, _ Artifact, report runreport.ArtifactReport) {
				report.ProjectionSizeBytes++
				metadata.reports[report.ArtifactID] = report
			},
			wantPhrase: "size does not match",
		},
		{
			name: "projection digest mismatch",
			configure: func(metadata *serviceMetadataStore, _ *serviceContentStore, _ Artifact, report runreport.ArtifactReport) {
				report.ProjectionSHA256 = strings.Repeat("a", 64)
				metadata.reports[report.ArtifactID] = report
			},
			wantPhrase: "digest does not match",
		},
		{
			name: "invalid projection",
			configure: func(metadata *serviceMetadataStore, contents *serviceContentStore, _ Artifact, report runreport.ArtifactReport) {
				projection := []byte(`{}`)
				digest := sha256.Sum256(projection)
				report.ProjectionSizeBytes = int64(len(projection))
				report.ProjectionSHA256 = hex.EncodeToString(digest[:])
				metadata.reports[report.ArtifactID] = report
				contents.objects[report.ProjectionObjectKey] = string(projection)
			},
			wantPhrase: "decode Run report projection",
		},
		{
			name: "summary mismatch",
			configure: func(metadata *serviceMetadataStore, _ *serviceContentStore, _ Artifact, report runreport.ArtifactReport) {
				summary := *report.Summary.(*runreport.TerraformPlanSummary)
				summary.ResourceChanges.Create = 0
				summary.ResourceChanges.NoOp = 1
				report.Summary = summary
				metadata.reports[report.ArtifactID] = report
			},
			wantPhrase: "summary does not match",
		},
		{
			name: "object store failure",
			configure: func(_ *serviceMetadataStore, contents *serviceContentStore, _ Artifact, _ runreport.ArtifactReport) {
				contents.err = objectFailure
			},
			wantError: objectFailure,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, record, report := reportReadService(t)
			metadata := service.artifacts.(*serviceMetadataStore)
			contents := service.contents.(*serviceContentStore)
			test.configure(metadata, contents, record, report)

			_, err := service.OpenReport(context.Background(), record.ID)
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("OpenReport() error = %v, want %v", err, test.wantError)
			}
			if test.wantPhrase != "" && (err == nil || !strings.Contains(err.Error(), test.wantPhrase)) {
				t.Fatalf("OpenReport() error = %v, want containing %q", err, test.wantPhrase)
			}
		})
	}
}

func reportReadService(t *testing.T) (*Service, Artifact, runreport.ArtifactReport) {
	t.Helper()

	createdAt := time.Date(2026, time.October, 29, 14, 0, 0, 0, time.UTC)
	report, err := runreport.DecodeTerraformPlan(strings.NewReader(`{
		"format_version":"1.0",
		"terraform_version":"1.14.3",
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
	projection, err := runreport.Encode(report)
	if err != nil {
		t.Fatal(err)
	}
	record := Artifact{
		ID:             uuid.New(),
		RunID:          uuid.New(),
		Name:           "plan-report",
		Kind:           KindOutput,
		State:          StateSealed,
		MediaType:      runreport.TerraformPlanMediaType,
		Sensitive:      true,
		RetentionState: RetentionRetained,
		CreatedAt:      createdAt,
		SealedAt:       &createdAt,
	}
	metadata, err := runreport.NewArtifactReport(
		record.ID,
		report,
		"runs/report/artifacts/plan-report/reports/terraform-plan-v1.json",
		projection,
		createdAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &serviceMetadataStore{
		record:  &record,
		reports: map[uuid.UUID]runreport.ArtifactReport{record.ID: metadata},
	}
	contents := &serviceContentStore{
		objects: map[string]string{metadata.ProjectionObjectKey: string(projection)},
	}

	return newTestService(store, contents), record, metadata
}
