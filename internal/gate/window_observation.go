package gate

import (
	"encoding/hex"
	"errors"
	"time"

	"github.com/slipramp/slipramp/internal/productidentity"
)

type ScopeWindow struct {
	Definition  WeeklyWindow      `json:"definition"`
	Digest      string            `json:"digest"`
	State       RequirementState  `json:"state"`
	Observation WindowObservation `json:"observation"`
}

type WindowEvent struct {
	Action     string
	Version    int64
	Actor      productidentity.ActorSnapshot
	RecordedAt time.Time
	Definition *WeeklyWindow
	Digest     *string
}

func (o WindowObservation) StateAt(at time.Time) RequirementState {
	if at.Before(o.EvaluatedAt) || !at.Before(o.ValidUntil) {
		return RequirementUnavailable
	}
	return o.State
}

type WindowEvidence struct {
	TimeZone    string            `json:"timeZone"`
	Digest      string            `json:"digest"`
	Observation WindowObservation `json:"observation"`
}

func (*WindowEvidence) Type() string { return WindowType }
func (w *WindowEvidence) Validate() error {
	if w == nil || w.TimeZone == "" || len(w.TimeZone) > 255 || len(w.Digest) != 64 {
		return errors.New("invalid window identity")
	}
	if _, err := hex.DecodeString(w.Digest); err != nil {
		return errors.New("invalid window digest")
	}
	o := w.Observation
	if o.State != RequirementPassing && o.State != RequirementBlocking || o.EvaluatedAt.IsZero() || !o.ValidUntil.After(o.EvaluatedAt) {
		return errors.New("invalid window observation")
	}
	if o.NextTransition != nil && !o.NextTransition.Equal(o.ValidUntil) || o.IntervalStart != nil &&
		(o.State != RequirementPassing || o.IntervalStart.After(o.EvaluatedAt)) {
		return errors.New("invalid window interval")
	}
	return nil
}
