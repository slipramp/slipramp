package gate

import (
	"time"

	"github.com/google/uuid"
)

type SummaryState string

const (
	SummaryNotRequired SummaryState = "not-required"
	SummaryWaiting     SummaryState = "waiting"
	SummaryReleased    SummaryState = "released"
	SummaryStopped     SummaryState = "stopped"
)

type RequirementState string

const (
	RequirementUnevaluated RequirementState = "unevaluated"
	RequirementPassing     RequirementState = RequirementState(ObservationPassing)
	RequirementBlocking    RequirementState = RequirementState(ObservationBlocking)
	RequirementUnavailable RequirementState = RequirementState(ObservationUnavailable)
	RequirementReleased    RequirementState = "released"
	RequirementStopped     RequirementState = "stopped"
)

// Summary lists requirements in attachment order. Its zero value represents ungated work.
type Summary []Requirement

func (s Summary) State() SummaryState {
	if len(s) == 0 {
		return SummaryNotRequired
	}
	for _, requirement := range s {
		if requirement.State == RequirementStopped {
			return SummaryStopped
		}
		if requirement.State != RequirementReleased {
			return SummaryWaiting
		}
	}

	return SummaryReleased
}

type Requirement struct {
	Name        string
	Title       string
	State       RequirementState
	Value       *float64
	Condition   *Condition
	EvaluatedAt *time.Time
	ReleasedAt  *time.Time
	Diagnostic  string
}

// RequirementEvidence holds the fields needed to show one Gate requirement.
// After release, its condition and observation come from the stored release evidence.
type RequirementEvidence struct {
	Canceled           bool
	Name               string
	Title              string
	Condition          *Condition
	Observation        Observation
	EvaluatedAt        *time.Time
	ReleasedAt         *time.Time
	EvaluatorProcessID uuid.UUID
	ValidUntil         time.Time
}

func (e RequirementEvidence) RequirementAt(processID uuid.UUID, at time.Time) Requirement {
	requirement := Requirement{Name: e.Name, Title: e.Title, Condition: e.Condition}
	if e.ReleasedAt != nil {
		requirement.State = RequirementReleased
		requirement.Value = e.Observation.Value
		requirement.EvaluatedAt = e.EvaluatedAt
		requirement.ReleasedAt = e.ReleasedAt
		return requirement
	}

	if e.Canceled {
		return Requirement{Name: e.Name, Title: e.Title, State: RequirementStopped}
	}

	switch {
	case e.Condition == nil:
		requirement.State = RequirementUnavailable
		requirement.Diagnostic = "The Gate definition is not available in the active catalog."
	case e.EvaluatedAt == nil || e.EvaluatorProcessID != processID:
		requirement.State = RequirementUnevaluated
	case e.EvaluatedAt.After(at):
		requirement.State = RequirementUnavailable
		requirement.EvaluatedAt = e.EvaluatedAt
		requirement.Diagnostic = "The latest Gate observation is in the future."
	case !e.ValidUntil.After(at):
		requirement.State = RequirementUnavailable
		requirement.EvaluatedAt = e.EvaluatedAt
		requirement.Diagnostic = "The latest Gate observation is stale."
	default:
		requirement.State = RequirementState(e.Observation.State)
		requirement.EvaluatedAt = e.EvaluatedAt
		requirement.Value = e.Observation.Value
		requirement.Diagnostic = e.Observation.Diagnostic
	}

	return requirement
}
