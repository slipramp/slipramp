package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/runreport"
)

func TestGetArtifactReportEnforcesSensitiveContentAuthorization(t *testing.T) {
	t.Parallel()

	content := handlerReportContent(t)
	tests := []struct {
		name       string
		role       productidentity.Role
		wantStatus string
	}{
		{name: "viewer", role: productidentity.RoleViewer, wantStatus: "forbidden"},
		{name: "operator", role: productidentity.RoleOperator, wantStatus: "success"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &recordingArtifactService{report: content}
			handler := newHandler(Dependencies{Artifacts: service})
			authentication := productAuthentication(
				test.role,
				productidentity.AuthenticationMethodWebSession,
				nil,
			)
			ctx := context.WithValue(
				context.Background(),
				productAuthenticationContextKey{},
				authentication,
			)
			response, err := handler.GetArtifactReport(ctx, api.GetArtifactReportRequestObject{
				ArtifactID: content.Artifact.ID,
			})
			if err != nil {
				t.Fatalf("GetArtifactReport() error = %v", err)
			}
			if service.reportCalls != 1 {
				t.Fatalf("OpenReport() calls = %d, want 1", service.reportCalls)
			}

			switch test.wantStatus {
			case "forbidden":
				if _, ok := response.(api.GetArtifactReport403ApplicationProblemPlusJSONResponse); !ok {
					t.Fatalf("GetArtifactReport() response = %T, want forbidden", response)
				}
			case "success":
				result, ok := response.(api.GetArtifactReport200JSONResponse)
				if !ok {
					t.Fatalf("GetArtifactReport() response = %T, want success", response)
				}
				plan, err := api.RunReport(result).AsTerraformPlanReport()
				if err != nil {
					t.Fatal(err)
				}
				if plan.ArtifactID != content.Artifact.ID || plan.RunID != content.Artifact.RunID ||
					plan.Summary.ResourceChanges.Create != 1 || len(plan.ResourceChanges) != 1 ||
					plan.ResourceChanges[0].Address != "example.main" ||
					plan.ResourceChanges[0].ReplacedObjectKey == nil ||
					*plan.ResourceChanges[0].ReplacedObjectKey != "deadbeef" {
					t.Errorf("GetArtifactReport() plan = %#v", plan)
				}
			}
		})
	}
}

func TestGetArtifactReportMapsMissingReport(t *testing.T) {
	t.Parallel()

	handler := newHandler(Dependencies{
		Artifacts: &recordingArtifactService{reportErr: artifact.ErrNotFound},
	})
	response, err := handler.GetArtifactReport(
		context.Background(),
		api.GetArtifactReportRequestObject{ArtifactID: uuid.New()},
	)
	if err != nil {
		t.Fatalf("GetArtifactReport() error = %v", err)
	}
	if _, ok := response.(api.GetArtifactReport404ApplicationProblemPlusJSONResponse); !ok {
		t.Fatalf("GetArtifactReport() response = %T, want not found", response)
	}
}

func TestArtifactResponseIncludesTypedReportSummary(t *testing.T) {
	t.Parallel()

	content := handlerReportContent(t)
	response, err := artifactResponse(artifact.Record{
		Artifact: content.Artifact,
		Report:   &content.Metadata,
	}, api.GitCommit{})
	if err != nil {
		t.Fatalf("artifactResponse() error = %v", err)
	}
	if response.Report == nil {
		t.Fatal("artifactResponse() report = nil")
	}
	summary, err := response.Report.AsTerraformPlanReportSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.ArtifactID != content.Artifact.ID || summary.RunID != content.Artifact.RunID ||
		summary.Summary.ResourceChanges.Create != 1 {
		t.Errorf("artifactResponse() summary = %#v", summary)
	}
}

func TestOpenTofuPlanReportResponses(t *testing.T) {
	t.Parallel()

	content := handlerOpenTofuReportContent(t)
	response, err := runReportResponse(content)
	if err != nil {
		t.Fatalf("runReportResponse() error = %v", err)
	}
	report, err := response.AsOpenTofuPlanReport()
	if err != nil {
		t.Fatal(err)
	}
	if report.Tool != "opentofu" || report.ToolVersion == nil || *report.ToolVersion != "1.13.2" ||
		report.Summary.ResourceChanges.Forget != 1 || len(report.ResourceChanges) != 1 ||
		report.ResourceChanges[0].Action != api.Forget {
		t.Errorf("runReportResponse() report = %#v", report)
	}

	artifactResponse, err := artifactResponse(artifact.Record{
		Artifact: content.Artifact,
		Report:   &content.Metadata,
	}, api.GitCommit{})
	if err != nil {
		t.Fatalf("artifactResponse() error = %v", err)
	}
	summary, err := artifactResponse.Report.AsOpenTofuPlanReportSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Tool != "opentofu" || summary.Summary.ResourceChanges.Forget != 1 {
		t.Errorf("artifactResponse() summary = %#v", summary)
	}
}

func TestPyinfraDeployReportResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		results     string
		wantMode    api.PyinfraDeployReportMode
		wantResults bool
	}{
		{
			name:     "preview",
			results:  "null",
			wantMode: api.PyinfraDeployReportModePreview,
		},
		{
			name: "apply",
			results: `{
				"operations":[{
					"op_hash":"package",
					"hosts":1,
					"success":["web-1"],
					"error":[],
					"no_change":[]
				}],
				"totals":{"hosts":1,"success":1,"error":0,"no_change":0},
				"failed_hosts":[]
			}`,
			wantMode:    api.PyinfraDeployReportModeApply,
			wantResults: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			content := handlerPyinfraReportContent(t, test.results)
			response, err := runReportResponse(content)
			if err != nil {
				t.Fatalf("runReportResponse() error = %v", err)
			}
			report, err := response.AsPyinfraDeployReport()
			if err != nil {
				t.Fatal(err)
			}
			if report.Mode != test.wantMode || (report.Summary.Results != nil) != test.wantResults ||
				len(report.Operations) != 1 || report.Operations[0].Names[0] != "server.package" ||
				(report.Operations[0].Result != nil) != test.wantResults {
				t.Errorf("runReportResponse() report = %#v", report)
			}

			artifactResponse, err := artifactResponse(artifact.Record{
				Artifact: content.Artifact,
				Report:   &content.Metadata,
			}, api.GitCommit{})
			if err != nil {
				t.Fatalf("artifactResponse() error = %v", err)
			}
			summary, err := artifactResponse.Report.AsPyinfraDeployReportSummary()
			if err != nil {
				t.Fatal(err)
			}
			if string(summary.Mode) != string(test.wantMode) ||
				(summary.Summary.Results != nil) != test.wantResults ||
				summary.Summary.Plan.ChangedOperations != 1 {
				t.Errorf("artifactResponse() summary = %#v", summary)
			}
		})
	}
}

func handlerReportContent(t *testing.T) artifact.ReportContent {
	t.Helper()

	report, err := runreport.DecodeTerraformPlan(strings.NewReader(`{
		"format_version":"1.0",
		"terraform_version":"1.14.3",
		"resource_changes":[{
			"address":"example.main",
			"deposed":"deadbeef",
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

	return newHandlerReportContent(
		t,
		&report,
		"plan-report",
		runreport.TerraformPlanMediaType,
		"runs/report/artifacts/plan-report/reports/terraform-plan-v1.json",
		time.Date(2026, time.October, 29, 15, 0, 0, 0, time.UTC),
	)
}

func handlerOpenTofuReportContent(t *testing.T) artifact.ReportContent {
	t.Helper()

	report, err := runreport.DecodeOpenTofuPlan(strings.NewReader(`{
		"format_version":"1.2",
		"terraform_version":"1.13.2",
		"errored":false,
		"resource_changes":[{
			"address":"terraform_data.forgotten",
			"mode":"managed",
			"type":"terraform_data",
			"name":"forgotten",
			"provider_name":"terraform.io/builtin/terraform",
			"change":{"actions":["forget"]}
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	return newHandlerReportContent(
		t,
		&report,
		"plan-report",
		runreport.OpenTofuPlanMediaType,
		"runs/report/artifacts/plan-report/reports/opentofu-plan-v1.json",
		time.Date(2026, time.October, 30, 15, 0, 0, 0, time.UTC),
	)
}

func handlerPyinfraReportContent(t *testing.T, results string) artifact.ReportContent {
	t.Helper()

	report, err := runreport.DecodePyinfraDeploy(strings.NewReader(`{
		"plan":[{
			"op_hash":"package",
			"names":["server.package"],
			"hosts_with_change":["web-1"],
			"hosts_with_conditional_change":[]
		}],
		"results":` + results + `
	}`))
	if err != nil {
		t.Fatal(err)
	}

	return newHandlerReportContent(
		t,
		&report,
		"deploy-report",
		runreport.PyinfraDeployMediaType,
		"runs/report/artifacts/deploy-report/reports/pyinfra-deploy-v1.json",
		time.Date(2026, time.October, 29, 16, 0, 0, 0, time.UTC),
	)
}

func newHandlerReportContent(
	t *testing.T,
	report runreport.Report,
	name string,
	mediaType string,
	projectionObjectKey string,
	createdAt time.Time,
) artifact.ReportContent {
	t.Helper()

	projection, err := runreport.Encode(report)
	if err != nil {
		t.Fatal(err)
	}
	record := artifact.Artifact{
		ID:             uuid.New(),
		RunID:          uuid.New(),
		Name:           name,
		Kind:           artifact.KindOutput,
		State:          artifact.StateSealed,
		MediaType:      mediaType,
		Sensitive:      true,
		RetentionState: artifact.RetentionRetained,
		CreatedAt:      createdAt,
		SealedAt:       &createdAt,
	}
	metadata, err := runreport.NewArtifactReport(
		record.ID,
		report,
		projectionObjectKey,
		projection,
		createdAt,
	)
	if err != nil {
		t.Fatal(err)
	}

	return artifact.ReportContent{Artifact: record, Metadata: metadata, Report: report}
}
