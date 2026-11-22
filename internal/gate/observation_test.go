package gate_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
)

func TestObservationValidation(t *testing.T) {
	t.Parallel()

	value := 1.0
	infiniteValue := math.Inf(1)
	tests := []struct {
		name        string
		observation gate.Observation
		wantError   bool
	}{
		{
			name:        "passing",
			observation: gate.Observation{State: gate.ObservationPassing, Evidence: gate.PrometheusSample(value)},
		},
		{
			name:        "blocking",
			observation: gate.Observation{State: gate.ObservationBlocking, Evidence: gate.PrometheusSample(value)},
		},
		{
			name: "unavailable",
			observation: gate.Observation{
				State:      gate.ObservationUnavailable,
				Diagnostic: "Prometheus request timed out.",
			},
		},
		{
			name:        "missing numeric value",
			observation: gate.Observation{State: gate.ObservationPassing},
			wantError:   true,
		},
		{
			name: "non-finite numeric value",
			observation: gate.Observation{
				State:    gate.ObservationBlocking,
				Evidence: gate.PrometheusSample(infiniteValue),
			},
			wantError: true,
		},
		{
			name: "numeric diagnostic",
			observation: gate.Observation{
				State:      gate.ObservationPassing,
				Evidence:   gate.PrometheusSample(value),
				Diagnostic: "unexpected",
			},
			wantError: true,
		},
		{
			name: "unavailable value",
			observation: gate.Observation{
				State:      gate.ObservationUnavailable,
				Evidence:   gate.PrometheusSample(value),
				Diagnostic: "unexpected",
			},
			wantError: true,
		},
		{
			name:        "missing unavailable diagnostic",
			observation: gate.Observation{State: gate.ObservationUnavailable},
			wantError:   true,
		},
		{
			name: "oversized diagnostic",
			observation: gate.Observation{
				State:      gate.ObservationUnavailable,
				Diagnostic: strings.Repeat("x", gate.MaximumDiagnosticLength+1),
			},
			wantError: true,
		},
		{
			name:        "unknown state",
			observation: gate.Observation{State: "unknown"},
			wantError:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.observation.Validate()
			if (err != nil) != test.wantError {
				t.Errorf("Validate() error = %v, want error %t", err, test.wantError)
			}
		})
	}
}

func TestCurrentObservationValidation(t *testing.T) {
	t.Parallel()

	evaluatedAt := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	value := 0.0
	valid := gate.CurrentObservation{
		RepositoryID:       uuid.New(),
		GateName:           "network-idle",
		CatalogRevisionID:  uuid.New(),
		DefinitionDigest:   strings.Repeat("a", 64),
		EvaluatorProcessID: uuid.New(),
		Observation:        gate.Observation{State: gate.ObservationPassing, Evidence: gate.PrometheusSample(value)},
		EvaluatedAt:        evaluatedAt,
		ValidUntil:         evaluatedAt.Add(45 * time.Second),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tests := []struct {
		name      string
		configure func(*gate.CurrentObservation)
	}{
		{name: "Repository ID", configure: func(value *gate.CurrentObservation) {
			value.RepositoryID = uuid.Nil
		}},
		{name: "Gate name", configure: func(value *gate.CurrentObservation) {
			value.GateName = "NOT_VALID"
		}},
		{name: "catalog revision ID", configure: func(value *gate.CurrentObservation) {
			value.CatalogRevisionID = uuid.Nil
		}},
		{name: "definition digest", configure: func(value *gate.CurrentObservation) {
			value.DefinitionDigest = strings.Repeat("A", 64)
		}},
		{name: "evaluator process ID", configure: func(value *gate.CurrentObservation) {
			value.EvaluatorProcessID = uuid.Nil
		}},
		{name: "evaluation time", configure: func(value *gate.CurrentObservation) {
			value.EvaluatedAt = time.Time{}
		}},
		{name: "validity window", configure: func(value *gate.CurrentObservation) {
			value.ValidUntil = value.EvaluatedAt
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := valid
			test.configure(&input)
			if err := input.Validate(); err == nil {
				t.Error("Validate() error = nil, want error")
			}
		})
	}
}
