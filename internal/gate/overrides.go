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
	ErrOverrideInvalid     = errors.New("a Gate override requires a definition digest and a reason of 1 to 1024 characters")
	ErrRequirementNotFound = errors.New("the Gate attachment does not exist")
	ErrOverrideConflict    = errors.New("the Gate is no longer waiting for this override or its definition has changed")
)

type SubjectType string

const (
	RunSubject       SubjectType = "run"
	PromotionSubject SubjectType = "promotion"
)

type OverrideRequest struct {
	Subject            SubjectType
	SubjectID          uuid.UUID
	GateName           string
	DefinitionDigest   string
	Reason             string
	Actor              productidentity.ActorSnapshot
	RecordedAt         time.Time
	EvaluatorProcessID uuid.UUID
}

func (r OverrideRequest) Validate() error {
	if (r.Subject != RunSubject && r.Subject != PromotionSubject) || r.SubjectID == uuid.Nil ||
		r.EvaluatorProcessID == uuid.Nil || r.RecordedAt.IsZero() || r.Actor.Validate() != nil ||
		validateDefinitionDigest(r.DefinitionDigest) != nil || strings.TrimSpace(r.Reason) == "" ||
		strings.ContainsRune(r.Reason, 0) || utf8.RuneCountInString(r.Reason) > 1024 {
		return ErrOverrideInvalid
	}
	return nil
}

type Override struct {
	DefinitionDigest string                        `json:"definitionDigest"`
	Reason           string                        `json:"reason"`
	Actor            productidentity.ActorSnapshot `json:"actor"`
	RecordedAt       time.Time                     `json:"recordedAt"`
	ObservedState    RequirementState              `json:"observedState"`
	ObservedValue    *float64                      `json:"observedValue,omitempty"`
	EvaluatedAt      *time.Time                    `json:"evaluatedAt,omitempty"`
	Diagnostic       string                        `json:"diagnostic,omitempty"`
}
