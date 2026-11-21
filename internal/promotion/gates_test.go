package promotion

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
)

func TestPromotionGateSummariesProjectCurrentAndReleasedRequirements(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 18, 0, 0, 0, time.UTC)
	processID := uuid.New()
	waitingPromotionID := uuid.New()
	releasedPromotionID := uuid.New()
	ungatedPromotionID := uuid.New()
	definition := testPromotionGateDefinition()
	digest, err := definition.Digest()
	if err != nil {
		t.Fatalf("digest Gate definition: %v", err)
	}
	observedValue := 1.0
	releaseValue := 0.0
	releasedAt := now.Add(-time.Minute)
	releaseEvaluatedAt := releasedAt.Add(-5 * time.Second)
	store := &reviewStore{gateProjections: []GateProjection{
		{
			PromotionID:      waitingPromotionID,
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
			PromotionID:        releasedPromotionID,
			Name:               "network-idle",
			ReleaseTitle:       "Network idle",
			ReleasedAt:         &releasedAt,
			ReleaseDefinition:  &definition,
			ReleaseValue:       &releaseValue,
			ReleaseEvaluatedAt: &releaseEvaluatedAt,
		},
	}}
	service := NewReviewService(nil, store, processID)

	summaries, err := service.GateSummaries(t.Context(), []Promotion{
		{ID: waitingPromotionID},
		{ID: releasedPromotionID},
		{ID: ungatedPromotionID},
	}, now)
	if err != nil {
		t.Fatalf("GateSummaries() error = %v", err)
	}

	waiting := summaries[waitingPromotionID]
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

	released := summaries[releasedPromotionID]
	if released.State != GateSummaryStateReleased || len(released.Requirements) != 1 ||
		released.Requirements[0].State != GateRequirementStateReleased ||
		released.Requirements[0].Title != "Network idle" || released.Requirements[0].ReleasedAt == nil ||
		!released.Requirements[0].ReleasedAt.Equal(releasedAt) {
		t.Errorf("released summary = %#v", released)
	}

	ungated := summaries[ungatedPromotionID]
	if ungated.State != GateSummaryStateNotRequired || len(ungated.Requirements) != 0 {
		t.Errorf("ungated summary = %#v", ungated)
	}
}

func TestPromotionGateSummariesFailClosedForUnavailableEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 18, 0, 0, 0, time.UTC)
	processID := uuid.New()
	definition := testPromotionGateDefinition()
	value := 0.0
	tests := []struct {
		name       string
		projection GateProjection
		wantState  GateRequirementState
		wantDiag   string
	}{
		{
			name:       "missing active definition",
			projection: GateProjection{Name: "network-idle"},
			wantState:  GateRequirementStateUnavailable,
			wantDiag:   missingGateDiagnostic,
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
			wantDiag:  staleGateDiagnostic,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			promotionID := uuid.New()
			test.projection.PromotionID = promotionID
			store := &reviewStore{gateProjections: []GateProjection{test.projection}}
			service := NewReviewService(nil, store, processID)
			summaries, err := service.GateSummaries(
				t.Context(),
				[]Promotion{{ID: promotionID}},
				now,
			)
			if err != nil {
				t.Fatalf("GateSummaries() error = %v", err)
			}
			requirement := summaries[promotionID].Requirements[0]
			if requirement.State != test.wantState || requirement.Diagnostic != test.wantDiag {
				t.Errorf("requirement = %#v", requirement)
			}
		})
	}
}

func TestPromotionGateSummariesPreserveAttachmentOrder(t *testing.T) {
	t.Parallel()

	promotionID := uuid.New()
	store := &reviewStore{gateProjections: []GateProjection{
		{PromotionID: promotionID, Name: "first"},
		{PromotionID: promotionID, Name: "second"},
	}}
	service := NewReviewService(nil, store, uuid.Nil)
	summaries, err := service.GateSummaries(
		t.Context(),
		[]Promotion{{ID: promotionID}},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("GateSummaries() error = %v", err)
	}
	names := []string{
		summaries[promotionID].Requirements[0].Name,
		summaries[promotionID].Requirements[1].Name,
	}
	if !reflect.DeepEqual(names, []string{"first", "second"}) {
		t.Errorf("Gate order = %v", names)
	}
}

func testPromotionGateDefinition() gate.Definition {
	return gate.Definition{Prometheus: gate.PrometheusDefinition{
		Source: "default",
		Query:  "active_users",
		Condition: gate.Condition{
			Operator: gate.OperatorEqual,
			Value:    0,
		},
	}}
}
