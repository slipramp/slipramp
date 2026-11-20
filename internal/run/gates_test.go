package run

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
)

func TestGateSummariesProjectCurrentAndReleasedRequirements(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 18, 0, 0, 0, time.UTC)
	processID := uuid.New()
	waitingRunID := uuid.New()
	releasedRunID := uuid.New()
	ungatedRunID := uuid.New()
	definition := testGateDefinition()
	digest, err := definition.Digest()
	if err != nil {
		t.Fatalf("digest Gate definition: %v", err)
	}
	observedValue := 1.0
	releaseValue := 0.0
	releasedAt := now.Add(-time.Minute)
	releaseEvaluatedAt := releasedAt.Add(-5 * time.Second)
	store := &stubGateStore{projections: []GateProjection{
		{
			RunID:            waitingRunID,
			Name:             "network-idle",
			ActiveTitle:      "Network idle",
			ActiveDefinition: &definition,
			CurrentObservation: &gate.CurrentObservation{
				RepositoryID:       uuid.New(),
				GateName:           "network-idle",
				CatalogRevisionID:  uuid.New(),
				DefinitionDigest:   digest,
				EvaluatorProcessID: processID,
				Observation: gate.Observation{
					State: gate.ObservationBlocking,
					Value: &observedValue,
				},
				EvaluatedAt: now.Add(-5 * time.Second),
				ValidUntil:  now.Add(time.Minute),
			},
		},
		{
			RunID:              releasedRunID,
			Name:               "network-idle",
			AttachedTitle:      "Network idle",
			ReleasedAt:         &releasedAt,
			ReleaseDefinition:  &definition,
			ReleaseValue:       &releaseValue,
			ReleaseEvaluatedAt: &releaseEvaluatedAt,
		},
	}}
	service := &Service{gates: store, gateProcessID: processID}

	summaries, err := service.GateSummaries(t.Context(), []Run{
		{ID: waitingRunID},
		{ID: releasedRunID},
		{ID: ungatedRunID},
	}, now)
	if err != nil {
		t.Fatalf("GateSummaries() error = %v", err)
	}

	waiting := summaries[waitingRunID]
	if waiting.State != GateSummaryStateWaiting || len(waiting.Requirements) != 1 {
		t.Fatalf("waiting summary = %#v", waiting)
	}
	requirement := waiting.Requirements[0]
	if requirement.State != GateRequirementStateBlocking || requirement.Title != "Network idle" ||
		requirement.Value == nil || *requirement.Value != observedValue ||
		requirement.Operator == nil || *requirement.Operator != gate.OperatorEqual ||
		requirement.Threshold == nil || *requirement.Threshold != 0 ||
		requirement.EvaluatedAt == nil || !requirement.EvaluatedAt.Equal(now.Add(-5*time.Second)) {
		t.Errorf("waiting requirement = %#v", requirement)
	}

	released := summaries[releasedRunID]
	if released.State != GateSummaryStateReleased || len(released.Requirements) != 1 ||
		released.Requirements[0].State != GateRequirementStateReleased ||
		released.Requirements[0].ReleasedAt == nil ||
		!released.Requirements[0].ReleasedAt.Equal(releasedAt) {
		t.Errorf("released summary = %#v", released)
	}

	ungated := summaries[ungatedRunID]
	if ungated.State != GateSummaryStateNotRequired || len(ungated.Requirements) != 0 {
		t.Errorf("ungated summary = %#v", ungated)
	}
}

func TestGateSummariesFailClosedForUnavailableEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 18, 0, 0, 0, time.UTC)
	processID := uuid.New()
	definition := testGateDefinition()
	value := 0.0
	tests := []struct {
		name       string
		projection GateProjection
		wantState  GateRequirementState
		wantTitle  string
		wantDiag   string
	}{
		{
			name: "missing active definition",
			projection: GateProjection{
				Name: "network-idle", AttachedTitle: "Network idle",
			},
			wantState: GateRequirementStateUnavailable,
			wantTitle: "Network idle",
			wantDiag:  missingGateDiagnostic,
		},
		{
			name: "not evaluated by this process",
			projection: GateProjection{
				Name: "network-idle", ActiveTitle: "Network idle", ActiveDefinition: &definition,
				CurrentObservation: &gate.CurrentObservation{
					EvaluatorProcessID: uuid.New(),
					Observation:        gate.Observation{State: gate.ObservationPassing, Value: &value},
					EvaluatedAt:        now.Add(-time.Second),
					ValidUntil:         now.Add(time.Minute),
				},
			},
			wantState: GateRequirementStateUnevaluated,
			wantTitle: "Network idle",
		},
		{
			name: "stale observation",
			projection: GateProjection{
				Name: "network-idle", ActiveTitle: "Network idle", ActiveDefinition: &definition,
				CurrentObservation: &gate.CurrentObservation{
					EvaluatorProcessID: processID,
					Observation:        gate.Observation{State: gate.ObservationPassing, Value: &value},
					EvaluatedAt:        now.Add(-time.Minute),
					ValidUntil:         now,
				},
			},
			wantState: GateRequirementStateUnavailable,
			wantTitle: "Network idle",
			wantDiag:  staleGateDiagnostic,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runID := uuid.New()
			test.projection.RunID = runID
			service := &Service{
				gates:         &stubGateStore{projections: []GateProjection{test.projection}},
				gateProcessID: processID,
			}
			summaries, err := service.GateSummaries(t.Context(), []Run{{ID: runID}}, now)
			if err != nil {
				t.Fatalf("GateSummaries() error = %v", err)
			}
			requirement := summaries[runID].Requirements[0]
			if requirement.State != test.wantState || requirement.Title != test.wantTitle ||
				requirement.Diagnostic != test.wantDiag {
				t.Errorf("requirement = %#v", requirement)
			}
		})
	}
}

func TestGateSummariesPreserveAttachmentOrder(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	service := &Service{gates: &stubGateStore{projections: []GateProjection{
		{RunID: runID, Name: "first"},
		{RunID: runID, Name: "second"},
	}}}
	summaries, err := service.GateSummaries(t.Context(), []Run{{ID: runID}}, time.Now())
	if err != nil {
		t.Fatalf("GateSummaries() error = %v", err)
	}
	names := []string{
		summaries[runID].Requirements[0].Name,
		summaries[runID].Requirements[1].Name,
	}
	if !reflect.DeepEqual(names, []string{"first", "second"}) {
		t.Errorf("Gate order = %v", names)
	}
}

func testGateDefinition() gate.Definition {
	return gate.Definition{Prometheus: gate.PrometheusDefinition{
		Source: "default",
		Query:  "active_users",
		Condition: gate.Condition{
			Operator: gate.OperatorEqual,
			Value:    0,
		},
	}}
}

type stubGateStore struct {
	projections []GateProjection
}

func (s *stubGateStore) ListRunGateProjections(
	context.Context,
	[]uuid.UUID,
) ([]GateProjection, error) {
	return s.projections, nil
}
