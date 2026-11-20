package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
)

const (
	GateSummaryStateNotRequired GateSummaryState = "not-required"
	GateSummaryStateWaiting     GateSummaryState = "waiting"
	GateSummaryStateReleased    GateSummaryState = "released"

	GateRequirementStateUnevaluated GateRequirementState = "unevaluated"
	GateRequirementStatePassing     GateRequirementState = "passing"
	GateRequirementStateBlocking    GateRequirementState = "blocking"
	GateRequirementStateUnavailable GateRequirementState = "unavailable"
	GateRequirementStateReleased    GateRequirementState = "released"
)

const (
	missingGateDiagnostic = "The Gate definition is not available in the active catalog."
	staleGateDiagnostic   = "The latest Gate observation is stale."
)

type GateSummaryState string

type GateRequirementState string

type GateSummary struct {
	State        GateSummaryState
	Requirements []GateRequirement
}

type GateRequirement struct {
	Name        string
	Title       string
	State       GateRequirementState
	Value       *float64
	Operator    *string
	Threshold   *float64
	EvaluatedAt *time.Time
	ReleasedAt  *time.Time
	Diagnostic  string
}

type GateProjection struct {
	RunID              uuid.UUID
	Name               string
	AttachedTitle      string
	ActiveTitle        string
	ActiveDefinition   *gate.Definition
	CurrentObservation *gate.CurrentObservation
	ReleasedAt         *time.Time
	ReleaseDefinition  *gate.Definition
	ReleaseValue       *float64
	ReleaseEvaluatedAt *time.Time
}

type GateStore interface {
	ListRunGateProjections(context.Context, []uuid.UUID) ([]GateProjection, error)
}

func (s *Service) GateSummaries(
	ctx context.Context,
	records []Run,
	at time.Time,
) (map[uuid.UUID]GateSummary, error) {
	result := make(map[uuid.UUID]GateSummary, len(records))
	ids := make([]uuid.UUID, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
		result[record.ID] = GateSummary{
			State:        GateSummaryStateNotRequired,
			Requirements: []GateRequirement{},
		}
	}
	if len(ids) == 0 {
		return result, nil
	}

	projections, err := s.gates.ListRunGateProjections(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load Run Gate projections: %w", err)
	}

	at = at.UTC()
	for _, projection := range projections {
		summary, found := result[projection.RunID]
		if !found {
			return nil, errors.New("Run Gate projection references an unrequested Run")
		}

		requirement := s.gateRequirement(projection, at)
		summary.Requirements = append(summary.Requirements, requirement)
		if summary.State == GateSummaryStateNotRequired {
			summary.State = GateSummaryStateReleased
		}
		if requirement.State != GateRequirementStateReleased {
			summary.State = GateSummaryStateWaiting
		}
		result[projection.RunID] = summary
	}

	return result, nil
}

func (s *Service) gateRequirement(projection GateProjection, at time.Time) GateRequirement {
	if projection.ReleasedAt != nil {
		requirement := GateRequirement{
			Name:        projection.Name,
			Title:       projection.AttachedTitle,
			State:       GateRequirementStateReleased,
			Value:       projection.ReleaseValue,
			EvaluatedAt: projection.ReleaseEvaluatedAt,
			ReleasedAt:  projection.ReleasedAt,
		}
		setGateCondition(&requirement, projection.ReleaseDefinition)

		return requirement
	}

	requirement := GateRequirement{
		Name:  projection.Name,
		Title: projection.ActiveTitle,
		State: GateRequirementStateUnevaluated,
	}
	if projection.ActiveDefinition == nil {
		requirement.Title = projection.AttachedTitle
		requirement.State = GateRequirementStateUnavailable
		requirement.Diagnostic = missingGateDiagnostic

		return requirement
	}
	setGateCondition(&requirement, projection.ActiveDefinition)

	observation := projection.CurrentObservation
	if observation == nil || observation.EvaluatorProcessID != s.gateProcessID {
		return requirement
	}
	requirement.EvaluatedAt = &observation.EvaluatedAt
	if !observation.ValidUntil.After(at) {
		requirement.State = GateRequirementStateUnavailable
		requirement.Diagnostic = staleGateDiagnostic

		return requirement
	}

	requirement.State = GateRequirementState(observation.Observation.State)
	requirement.Value = observation.Observation.Value
	requirement.Diagnostic = observation.Observation.Diagnostic

	return requirement
}

func setGateCondition(requirement *GateRequirement, definition *gate.Definition) {
	if definition == nil {
		return
	}

	operator := definition.Prometheus.Condition.Operator
	threshold := definition.Prometheus.Condition.Value
	requirement.Operator = &operator
	requirement.Threshold = &threshold
}
