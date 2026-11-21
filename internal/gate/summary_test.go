package gate

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRequirementAt(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	processID := uuid.New()
	noObservation := RequirementEvidence{
		Name: "idle", Title: "Network idle", Condition: &Condition{Operator: OperatorEqual},
	}
	passing := noObservation
	passing.EvaluatorProcessID = processID
	passing.EvaluatedAt = &now
	passing.ValidUntil = now.Add(time.Minute)
	passing.Observation = Observation{State: ObservationPassing, Value: new(0.0)}

	noDefinition := passing
	noDefinition.Condition = nil
	oldProcess := passing
	oldProcess.EvaluatorProcessID = uuid.New()
	stale := passing
	stale.EvaluatedAt = new(now.Add(-time.Minute))
	stale.ValidUntil = now
	future := passing
	future.EvaluatedAt = new(now.Add(time.Second))
	unavailable := passing
	unavailable.Observation = Observation{State: ObservationUnavailable, Diagnostic: "Prometheus request timed out."}
	blocking := passing
	blocking.Observation = Observation{State: ObservationBlocking, Value: new(1.0)}
	released := stale
	released.ReleasedAt = &now
	released.EvaluatorProcessID = oldProcess.EvaluatorProcessID

	for _, tc := range []struct {
		name        string
		evidence    RequirementEvidence
		state       RequirementState
		value       *float64
		evaluatedAt *time.Time
		diagnostic  string
	}{
		{
			name: "missing definition", evidence: noDefinition, state: RequirementUnavailable,
			diagnostic: "The Gate definition is not available in the active catalog.",
		},
		{name: "no observation", evidence: noObservation, state: RequirementUnevaluated},
		{name: "earlier process", evidence: oldProcess, state: RequirementUnevaluated},
		{
			name: "expired", evidence: stale, state: RequirementUnavailable,
			evaluatedAt: stale.EvaluatedAt, diagnostic: "The latest Gate observation is stale.",
		},
		{
			name: "future", evidence: future, state: RequirementUnavailable,
			evaluatedAt: future.EvaluatedAt, diagnostic: "The latest Gate observation is in the future.",
		},
		{
			name: "source unavailable", evidence: unavailable, state: RequirementUnavailable,
			evaluatedAt: &now, diagnostic: unavailable.Observation.Diagnostic,
		},
		{
			name: "passing at start", evidence: passing, state: RequirementPassing,
			value: passing.Observation.Value, evaluatedAt: &now,
		},
		{
			name: "blocking", evidence: blocking, state: RequirementBlocking,
			value: blocking.Observation.Value, evaluatedAt: &now,
		},
		{
			name: "released with historical evidence", evidence: released, state: RequirementReleased,
			value: released.Observation.Value, evaluatedAt: released.EvaluatedAt,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := Requirement{
				Name: tc.evidence.Name, Title: tc.evidence.Title, Condition: tc.evidence.Condition,
				State: tc.state, Diagnostic: tc.diagnostic, Value: tc.value,
				EvaluatedAt: tc.evaluatedAt, ReleasedAt: tc.evidence.ReleasedAt,
			}
			if got := tc.evidence.RequirementAt(processID, now); !reflect.DeepEqual(got, want) {
				t.Errorf("requirement = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCanceledGateSummaryPreservesOnlyReleaseEvidence(t *testing.T) {
	now := time.Now().UTC()
	evidence := RequirementEvidence{
		Name: "idle", Canceled: true, Condition: &Condition{Operator: OperatorEqual},
		EvaluatedAt: &now, Observation: Observation{State: ObservationPassing, Value: new(0.0)},
	}
	stopped := Summary{evidence.RequirementAt(uuid.Nil, now)}
	if stopped.State() != SummaryStopped || !reflect.DeepEqual(stopped[0], Requirement{Name: "idle", State: RequirementStopped}) {
		t.Fatalf("unreleased cancellation = %#v", stopped)
	}
	evidence.ReleasedAt = &now
	released := Summary{evidence.RequirementAt(uuid.Nil, now)}
	if released.State() != SummaryReleased || released[0].Value != evidence.Observation.Value || released[0].ReleasedAt != &now {
		t.Fatalf("released cancellation = %#v", released)
	}
}
