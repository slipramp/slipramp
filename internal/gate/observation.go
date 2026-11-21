package gate

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	MaximumDiagnosticLength             = 512
	DefaultObservationCleanupInterval   = time.Hour
	DefaultInactiveObservationRetention = 24 * time.Hour
)

type ObservationState string

const (
	ObservationPassing     ObservationState = "passing"
	ObservationBlocking    ObservationState = "blocking"
	ObservationUnavailable ObservationState = "unavailable"
)

// Observation contains only the normalized value and a bounded diagnostic.
// Prometheus labels and response bodies never leave the evaluator.
type Observation struct {
	State      ObservationState
	Value      *float64
	Diagnostic string
}

// CurrentObservation is the replaceable evidence for one active Gate
// definition. Release also requires the evaluator process identity and
// validity window to match the current Server process and time.
type CurrentObservation struct {
	RepositoryID       uuid.UUID
	GateName           string
	CatalogRevisionID  uuid.UUID
	DefinitionDigest   string
	EvaluatorProcessID uuid.UUID
	Observation        Observation
	EvaluatedAt        time.Time
	ValidUntil         time.Time
}

func (o CurrentObservation) Validate() error {
	if o.RepositoryID == uuid.Nil {
		return errors.New( //nolint:staticcheck // Gate and Repository are domain names.
			"Gate observation Repository ID is required",
		)
	}
	if err := kubevalidate.DNSLabel("Gate observation name", o.GateName); err != nil {
		return err
	}
	if o.CatalogRevisionID == uuid.Nil {
		return errors.New( //nolint:staticcheck // Gate is a domain name.
			"Gate observation catalog revision ID is required",
		)
	}
	if err := validateDefinitionDigest(o.DefinitionDigest); err != nil {
		return err
	}
	if o.EvaluatorProcessID == uuid.Nil {
		return errors.New( //nolint:staticcheck // Gate is a domain name.
			"Gate observation evaluator process ID is required",
		)
	}
	if err := o.Observation.Validate(); err != nil {
		return err
	}
	if o.EvaluatedAt.IsZero() {
		return errors.New("Gate observation evaluation time is required") //nolint:staticcheck // Gate is a domain name.
	}
	if !o.ValidUntil.After(o.EvaluatedAt) {
		return errors.New( //nolint:staticcheck // Gate is a domain name.
			"Gate observation validity must end after its evaluation time",
		)
	}

	return nil
}

func (o Observation) Validate() error {
	switch o.State {
	case ObservationPassing, ObservationBlocking:
		if o.Value == nil || !isFinite(*o.Value) {
			return errors.New("passing or blocking Gate observation requires a finite value")
		}
		if o.Diagnostic != "" {
			return errors.New("passing or blocking Gate observation must not include a diagnostic")
		}
	case ObservationUnavailable:
		if o.Value != nil {
			return errors.New("unavailable Gate observation must not include a value")
		}
		if strings.TrimSpace(o.Diagnostic) == "" {
			return errors.New("unavailable Gate observation requires a diagnostic")
		}
		if utf8.RuneCountInString(o.Diagnostic) > MaximumDiagnosticLength {
			return fmt.Errorf( //nolint:staticcheck // Gate is a domain name.
				"Gate observation diagnostic must not exceed %d characters",
				MaximumDiagnosticLength,
			)
		}
	default:
		return fmt.Errorf( //nolint:staticcheck // Gate is a domain name.
			"Gate observation state %q is not supported",
			o.State,
		)
	}

	return nil
}

func validateDefinitionDigest(value string) error {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 || strings.ToLower(value) != value {
		return errors.New( //nolint:staticcheck // Gate is a domain name.
			"Gate observation definition digest must be 64 lowercase hexadecimal characters",
		)
	}

	return nil
}
