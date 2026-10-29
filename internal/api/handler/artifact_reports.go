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

func runReportResponse(content artifact.ReportContent) (api.RunReport, error) {
	report, ok := content.Report.(*runreport.TerraformPlanReport)
	if !ok {
		return api.RunReport{}, fmt.Errorf("run report kind %q is not supported", content.Metadata.Kind)
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
