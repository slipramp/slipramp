package gate

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

var (
	ErrOverrideInvalid  = errors.New("a Gate override requires a valid Gate key, a policy version, and a reason of 1 to 1024 characters")
	ErrSubjectNotFound  = errors.New("the Gate subject does not exist")
	ErrOverrideConflict = errors.New("the Gate is no longer eligible for this override or its version has changed")
)

type SubjectType string

const (
	RunSubject       SubjectType = "run"
	PromotionSubject SubjectType = "promotion"
)

type OverrideRequest struct {
	Subject   SubjectType
	SubjectID uuid.UUID
	Reference
	Reason             string
	Actor              productidentity.ActorSnapshot
	RecordedAt         time.Time
	EvaluatorProcessID uuid.UUID
}

func (r OverrideRequest) Validate() error {
	if (r.Subject != RunSubject && r.Subject != PromotionSubject) || r.SubjectID == uuid.Nil ||
		r.EvaluatorProcessID == uuid.Nil || r.RecordedAt.IsZero() || r.Actor.Validate() != nil {
		return ErrOverrideInvalid
	}
	_, _, server := r.Key.ServerScope()
	if (!server && !r.Key.validRepositoryKey()) || !r.Version.valid() ||
		strings.TrimSpace(r.Reason) == "" || strings.ContainsRune(r.Reason, 0) || utf8.RuneCountInString(r.Reason) > 1024 {
		return ErrOverrideInvalid
	}
	return nil
}

type Override struct {
	Reference
	Decision
}

// Decision captures the immutable operator action and evidence at decision time.
type Decision struct {
	Reason           string                        `json:"reason"`
	Actor            productidentity.ActorSnapshot `json:"actor"`
	RecordedAt       time.Time                     `json:"recordedAt"`
	ObservedState    RequirementState              `json:"observedState"`
	ObservedEvidence *Evidence                     `json:"observedEvidence,omitempty"`
	EvaluatedAt      *time.Time                    `json:"evaluatedAt,omitempty"`
	Diagnostic       string                        `json:"diagnostic,omitempty"`
}

type OverrideCollection struct {
	Eligible  []Requirement
	Decisions []Override
}
