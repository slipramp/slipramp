package handler

import (
	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
)

func gateSummaryResponse(summary gate.Summary) api.GateSummary {
	requirements := make([]api.GateRequirement, 0, len(summary))
	for _, requirement := range summary {
		var operator *api.GateOperator
		var threshold *float64
		if requirement.Condition != nil {
			operator = new(api.GateOperator(requirement.Condition.Operator))
			threshold = &requirement.Condition.Value
		}
		requirements = append(requirements, api.GateRequirement{
			Name:        api.CatalogName(requirement.Name),
			Title:       optionalString(requirement.Title),
			State:       api.GateRequirementState(requirement.State),
			Value:       requirement.Value,
			Operator:    operator,
			Threshold:   threshold,
			EvaluatedAt: requirement.EvaluatedAt,
			ReleasedAt:  requirement.ReleasedAt,
			Diagnostic:  optionalString(requirement.Diagnostic),
		})
	}

	return api.GateSummary{
		State:        api.GateSummaryState(summary.State()),
		Requirements: requirements,
	}
}
