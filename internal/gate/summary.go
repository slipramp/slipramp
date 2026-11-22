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

// Summary orders scope locks and windows before attached repository Gates. Its zero value represents ungated work.
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
	Reference
	Origin      Origin
	Scope       Scope
	Type        string
	Overrides   []Override
	Name        string
	Title       string
	State       RequirementState
	Evidence    *Evidence
	Policy      Policy
	EvaluatedAt *time.Time
	ValidUntil  *time.Time
	ReleasedAt  *time.Time
	Diagnostic  string
}

// RequirementEvidence holds the fields needed to show one Gate requirement.
// After release, its definition and observation come from the stored release evidence.
type RequirementEvidence struct {
	RepositoryID       uuid.UUID
	DefinitionDigest   string
	Canceled           bool
	Name               string
	Title              string
	Policy             Policy
	Observation        Observation
	EvaluatedAt        *time.Time
	ReleasedAt         *time.Time
	EvaluatorProcessID uuid.UUID
	ValidUntil         time.Time
}

func (e RequirementEvidence) RequirementAt(processID uuid.UUID, at time.Time) Requirement {
	requirement := Requirement{
		Name: e.Name, Title: e.Title, Origin: RepositoryOrigin,
		Scope: Scope{Kind: RepositoryScope, ID: e.RepositoryID},
		Key:   RepositoryKey(e.Name),
	}
	if e.Canceled && e.ReleasedAt == nil {
		requirement.State = RequirementStopped
		return requirement
	}
	if e.Policy != nil {
		requirement.Policy = e.Policy
		requirement.Type = e.Policy.Type()
		requirement.Version = Version(e.DefinitionDigest)
	}
	if e.ReleasedAt != nil {
		requirement.State = RequirementReleased
		requirement.Evidence = e.Observation.Evidence
		requirement.EvaluatedAt = e.EvaluatedAt
		if e.EvaluatedAt != nil {
			requirement.ValidUntil = &e.ValidUntil
		}
		requirement.ReleasedAt = e.ReleasedAt
		return requirement
	}

	if e.Policy == nil {
		requirement.State = RequirementUnavailable
		requirement.Diagnostic = "The Gate definition is not available in the active catalog."
		return requirement
	}
	if e.EvaluatedAt == nil || e.EvaluatorProcessID != processID {
		requirement.State = RequirementUnevaluated
		return requirement
	}
	requirement.EvaluatedAt = e.EvaluatedAt
	requirement.ValidUntil = &e.ValidUntil
	switch {
	case e.EvaluatedAt.After(at):
		requirement.State = RequirementUnavailable
		requirement.Diagnostic = "The latest Gate observation is in the future."
	case !e.ValidUntil.After(at):
		requirement.State = RequirementUnavailable
		requirement.Diagnostic = "The latest Gate observation is stale."
	default:
		requirement.State = RequirementState(e.Observation.State)
		requirement.Evidence = e.Observation.Evidence
		requirement.Diagnostic = e.Observation.Diagnostic
	}

	return requirement
}
