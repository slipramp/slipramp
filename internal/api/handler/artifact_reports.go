package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/runreport"
)

func (h *Handler) GetArtifactReport(
	ctx context.Context,
	request api.GetArtifactReportRequestObject,
) (api.GetArtifactReportResponseObject, error) {
	content, err := h.artifacts.OpenReport(ctx, request.ArtifactID)
	if errors.Is(err, artifact.ErrNotFound) || errors.Is(err, artifact.ErrNotSealed) {
		problem := NewProblem(http.StatusNotFound, "The artifact report does not exist or is not retained.")
		return api.GetArtifactReport404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	if content.Artifact.Sensitive && !canDownloadSensitiveArtifact(ctx) {
		problem := NewProblem(
			http.StatusForbidden,
			"The authenticated principal cannot read sensitive artifact report contents.",
		)

		return api.GetArtifactReport403ApplicationProblemPlusJSONResponse(problem), nil
	}

	response, err := runReportResponse(content)
	if err != nil {
		return nil, err
	}

	return api.GetArtifactReport200JSONResponse(response), nil
}

func reportSummaryResponse(record artifact.Record) (*api.RunReportSummary, error) {
	if record.Report == nil {
		return nil, nil
	}

	switch record.Report.Kind {
	case runreport.TerraformPlanKind:
		return terraformPlanReportSummaryResponse(record)
	case runreport.PyinfraDeployKind:
		return pyinfraDeployReportSummaryResponse(record)
	default:
		return nil, fmt.Errorf("run report kind %q is not supported", record.Report.Kind)
	}
}

func runReportResponse(content artifact.ReportContent) (api.RunReport, error) {
	switch report := content.Report.(type) {
	case *runreport.TerraformPlanReport:
		return terraformPlanReportResponse(content, report)
	case *runreport.PyinfraDeployReport:
		return pyinfraDeployReportResponse(content, report)
	default:
		return api.RunReport{}, fmt.Errorf("run report kind %q is not supported", content.Metadata.Kind)
	}
}

func terraformPlanReportSummaryResponse(record artifact.Record) (*api.RunReportSummary, error) {
	summary, err := terraformPlanSummaryResponse(record.Report.Summary)
	if err != nil {
		return nil, err
	}
	body := api.TerraformPlanReportSummary{
		ArtifactID:     record.ID,
		RunID:          record.RunID,
		Kind:           api.TerraformPlanReportSummaryKind(record.Report.Kind),
		SchemaVersion:  record.Report.SchemaVersion,
		AdapterVersion: record.Report.AdapterVersion,
		Tool:           record.Report.Tool,
		ToolVersion:    optionalString(record.Report.ToolVersion),
		Mode:           api.TerraformPlanReportSummaryMode(record.Report.Mode),
		CreatedAt:      record.Report.CreatedAt,
		Summary:        summary,
	}
	response := &api.RunReportSummary{}
	if err := response.FromTerraformPlanReportSummary(body); err != nil {
		return nil, fmt.Errorf("encode Terraform plan report summary response: %w", err)
	}

	return response, nil
}

func terraformPlanReportResponse(
	content artifact.ReportContent,
	report *runreport.TerraformPlanReport,
) (api.RunReport, error) {
	if report == nil {
		return api.RunReport{}, errors.New("terraform plan report is missing")
	}
	summary, err := terraformPlanSummaryResponse(&report.Summary)
	if err != nil {
		return api.RunReport{}, err
	}
	body := api.TerraformPlanReport{
		ArtifactID:      content.Artifact.ID,
		RunID:           content.Artifact.RunID,
		Kind:            api.TerraformPlanReportKind(report.Kind),
		SchemaVersion:   report.SchemaVersion,
		AdapterVersion:  report.AdapterVersion,
		Tool:            report.Tool,
		ToolVersion:     optionalString(report.ToolVersion),
		Mode:            api.TerraformPlanReportMode(report.Mode),
		CreatedAt:       content.Metadata.CreatedAt,
		Summary:         summary,
		ResourceChanges: terraformResourceChangeResponses(report.ResourceChanges),
		ResourceDrift:   terraformResourceChangeResponses(report.ResourceDrift),
	}
	response := api.RunReport{}
	if err := response.FromTerraformPlanReport(body); err != nil {
		return api.RunReport{}, fmt.Errorf("encode Terraform plan report response: %w", err)
	}

	return response, nil
}

func pyinfraDeployReportSummaryResponse(record artifact.Record) (*api.RunReportSummary, error) {
	summary, err := pyinfraDeploySummaryResponse(record.Report.Summary)
	if err != nil {
		return nil, err
	}
	body := api.PyinfraDeployReportSummary{
		ArtifactID:     record.ID,
		RunID:          record.RunID,
		Kind:           api.PyinfraDeployReportSummaryKind(record.Report.Kind),
		SchemaVersion:  record.Report.SchemaVersion,
		AdapterVersion: record.Report.AdapterVersion,
		Tool:           record.Report.Tool,
		ToolVersion:    optionalString(record.Report.ToolVersion),
		Mode:           api.PyinfraDeployReportSummaryMode(record.Report.Mode),
		CreatedAt:      record.Report.CreatedAt,
		Summary:        summary,
	}
	response := &api.RunReportSummary{}
	if err := response.FromPyinfraDeployReportSummary(body); err != nil {
		return nil, fmt.Errorf("encode pyinfra deploy report summary response: %w", err)
	}

	return response, nil
}

func pyinfraDeployReportResponse(
	content artifact.ReportContent,
	report *runreport.PyinfraDeployReport,
) (api.RunReport, error) {
	if report == nil {
		return api.RunReport{}, errors.New("pyinfra deploy report is missing")
	}
	summary, err := pyinfraDeploySummaryResponse(&report.Summary)
	if err != nil {
		return api.RunReport{}, err
	}
	body := api.PyinfraDeployReport{
		ArtifactID:     content.Artifact.ID,
		RunID:          content.Artifact.RunID,
		Kind:           api.PyinfraDeployReportKind(report.Kind),
		SchemaVersion:  report.SchemaVersion,
		AdapterVersion: report.AdapterVersion,
		Tool:           report.Tool,
		ToolVersion:    optionalString(report.ToolVersion),
		Mode:           api.PyinfraDeployReportMode(report.Mode),
		CreatedAt:      content.Metadata.CreatedAt,
		Summary:        summary,
		Operations:     pyinfraOperationResponses(report.Operations),
		FailedHosts:    nonNilStrings(report.FailedHosts),
	}
	response := api.RunReport{}
	if err := response.FromPyinfraDeployReport(body); err != nil {
		return api.RunReport{}, fmt.Errorf("encode pyinfra deploy report response: %w", err)
	}

	return response, nil
}

func pyinfraDeploySummaryResponse(value runreport.Summary) (api.PyinfraDeploySummary, error) {
	summary, ok := value.(*runreport.PyinfraDeploySummary)
	if !ok {
		return api.PyinfraDeploySummary{}, fmt.Errorf("run report summary type %T is not supported", value)
	}
	if summary == nil {
		return api.PyinfraDeploySummary{}, errors.New("pyinfra deploy report summary is missing")
	}

	response := api.PyinfraDeploySummary{
		Plan: api.PyinfraPlanSummary{
			TotalOperations:           summary.Plan.TotalOperations,
			ChangedOperations:         summary.Plan.ChangedOperations,
			UnchangedOperations:       summary.Plan.UnchangedOperations,
			AffectedHosts:             summary.Plan.AffectedHosts,
			PlannedHostOperations:     summary.Plan.PlannedHostOperations,
			ConditionalHostOperations: summary.Plan.ConditionalHostOperations,
		},
	}
	if summary.Results != nil {
		response.Results = &api.PyinfraResultSummary{
			HostOperations:          summary.Results.HostOperations,
			ChangedHostOperations:   summary.Results.ChangedHostOperations,
			FailedHostOperations:    summary.Results.FailedHostOperations,
			UnchangedHostOperations: summary.Results.UnchangedHostOperations,
			FailedHosts:             summary.Results.FailedHosts,
		}
	}

	return response, nil
}

func pyinfraOperationResponses(operations []runreport.PyinfraOperation) []api.PyinfraOperation {
	result := make([]api.PyinfraOperation, 0, len(operations))
	for _, operation := range operations {
		response := api.PyinfraOperation{
			Names:                       nonNilStrings(operation.Names),
			HostsWithChanges:            nonNilStrings(operation.HostsWithChanges),
			HostsWithConditionalChanges: nonNilStrings(operation.HostsWithConditionalChanges),
		}
		if operation.Result != nil {
			response.Result = &api.PyinfraOperationResult{
				ChangedHosts:   nonNilStrings(operation.Result.ChangedHosts),
				FailedHosts:    nonNilStrings(operation.Result.FailedHosts),
				UnchangedHosts: nonNilStrings(operation.Result.UnchangedHosts),
			}
		}
		result = append(result, response)
	}

	return result
}

func nonNilStrings(values []string) []string {
	return append([]string{}, values...)
}

func terraformPlanSummaryResponse(value runreport.Summary) (api.TerraformPlanSummary, error) {
	summary, ok := value.(*runreport.TerraformPlanSummary)
	if !ok {
		return api.TerraformPlanSummary{}, fmt.Errorf("run report summary type %T is not supported", value)
	}
	if summary == nil {
		return api.TerraformPlanSummary{}, errors.New("terraform plan report summary is missing")
	}

	return api.TerraformPlanSummary{
		ResourceChanges:  terraformActionCountsResponse(summary.ResourceChanges),
		DriftedResources: summary.DriftedResources,
		Checks: api.TerraformCheckCounts{
			Total:   summary.Checks.Total,
			Pass:    summary.Checks.Pass,
			Fail:    summary.Checks.Fail,
			Error:   summary.Checks.Error,
			Unknown: summary.Checks.Unknown,
		},
		OutputChanges: terraformActionCountsResponse(summary.OutputChanges),
		Applyable:     summary.Applyable,
		Complete:      summary.Complete,
		Errored:       summary.Errored,
	}, nil
}

func terraformActionCountsResponse(counts runreport.TerraformActionCounts) api.TerraformActionCounts {
	return api.TerraformActionCounts{
		Total:   counts.Total,
		NoOp:    counts.NoOp,
		Create:  counts.Create,
		Read:    counts.Read,
		Update:  counts.Update,
		Delete:  counts.Delete,
		Replace: counts.Replace,
	}
}

func terraformResourceChangeResponses(changes []runreport.TerraformResourceChange) []api.TerraformResourceChange {
	result := make([]api.TerraformResourceChange, 0, len(changes))
	for _, change := range changes {
		var replacementOrder *api.TerraformResourceChangeReplacementOrder
		if change.ReplacementOrder != "" {
			value := api.TerraformResourceChangeReplacementOrder(change.ReplacementOrder)
			replacementOrder = &value
		}
		result = append(result, api.TerraformResourceChange{
			Address:           change.Address,
			ReplacedObjectKey: optionalString(change.ReplacedObjectKey),
			PreviousAddress:   optionalString(change.PreviousAddress),
			ModuleAddress:     optionalString(change.ModuleAddress),
			Mode:              change.Mode,
			Type:              change.Type,
			Name:              change.Name,
			ProviderName:      change.ProviderName,
			Action:            api.TerraformResourceChangeAction(change.Action),
			ReplacementOrder:  replacementOrder,
			ActionReason:      optionalString(change.ActionReason),
		})
	}

	return result
}
