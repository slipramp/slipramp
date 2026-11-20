package gate

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCoordinatorReconcilesDemandAtDefinitionCadence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	definition := testEvaluationDefinition("network_active_changes")
	first := EvaluationTarget{
		RepositoryID:      uuid.New(),
		GateName:          "network-idle",
		CatalogRevisionID: uuid.New(),
		Definition:        definition,
	}
	second := EvaluationTarget{
		RepositoryID:      uuid.New(),
		GateName:          "change-window",
		CatalogRevisionID: uuid.New(),
		Definition:        definition,
	}
	store := &recordingEvaluationStore{targets: []EvaluationTarget{first, second}}
	evaluator := &recordingDefinitionEvaluator{
		observation: Observation{State: ObservationPassing, Value: new(float64)},
	}
	coordinator := newCoordinator(
		store,
		testEvaluationSources(),
		map[string]definitionEvaluator{"homelab": evaluator},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	processID := uuid.New()
	coordinator.processID = processID
	coordinator.now = func() time.Time { return now }

	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(evaluator.calls) != 1 {
		t.Fatalf("definition evaluations = %d, want 1", len(evaluator.calls))
	}
	if len(store.observations) != 2 {
		t.Fatalf("recorded observations = %d, want 2", len(store.observations))
	}
	for _, observation := range store.observations {
		if observation.EvaluatorProcessID != processID ||
			!observation.EvaluatedAt.Equal(now) ||
			!observation.ValidUntil.Equal(now.Add(45*time.Second)) {
			t.Errorf("recorded observation = %#v", observation)
		}
	}

	now = now.Add(14 * time.Second)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() before interval error = %v", err)
	}
	if len(evaluator.calls) != 1 || len(store.observations) != 2 {
		t.Errorf("early reconciliation evaluated %d times and recorded %d observations",
			len(evaluator.calls), len(store.observations))
	}

	now = now.Add(time.Second)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() at interval error = %v", err)
	}
	if len(evaluator.calls) != 2 || len(store.observations) != 4 {
		t.Errorf("due reconciliation evaluated %d times and recorded %d observations",
			len(evaluator.calls), len(store.observations))
	}

	third := first
	third.RepositoryID = uuid.New()
	third.GateName = "maintenance-idle"
	store.targets = append(store.targets, third)
	now = now.Add(time.Second)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() with new waiter error = %v", err)
	}
	if len(evaluator.calls) != 3 || len(store.observations) != 7 {
		t.Errorf("new waiter reconciliation evaluated %d times and recorded %d observations",
			len(evaluator.calls), len(store.observations))
	}

	store.targets = nil
	now = now.Add(time.Second)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() without waiters error = %v", err)
	}
	if len(coordinator.states) != 0 || len(evaluator.calls) != 3 {
		t.Errorf("idle coordinator states = %d, evaluations = %d", len(coordinator.states), len(evaluator.calls))
	}

	store.targets = []EvaluationTarget{first}
	now = now.Add(time.Second)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() with returning waiter error = %v", err)
	}
	if len(evaluator.calls) != 4 {
		t.Errorf("returning waiter evaluations = %d, want 4", len(evaluator.calls))
	}
}

func TestCoordinatorSeparatesDistinctDefinitions(t *testing.T) {
	t.Parallel()

	first := EvaluationTarget{
		RepositoryID:      uuid.New(),
		GateName:          "network-idle",
		CatalogRevisionID: uuid.New(),
		Definition:        testEvaluationDefinition("network_active_changes"),
	}
	second := first
	second.GateName = "change-window"
	second.Definition = testEvaluationDefinition("change_window_open")
	store := &recordingEvaluationStore{targets: []EvaluationTarget{first, second}}
	evaluator := &recordingDefinitionEvaluator{
		observation: Observation{State: ObservationUnavailable, Diagnostic: "Prometheus request timed out."},
	}
	coordinator := newCoordinator(
		store,
		testEvaluationSources(),
		map[string]definitionEvaluator{"homelab": evaluator},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	coordinator.now = func() time.Time {
		return time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	}

	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(evaluator.calls) != 2 || len(store.observations) != 2 {
		t.Fatalf("evaluations = %d, observations = %d, want 2 each",
			len(evaluator.calls), len(store.observations))
	}
	if !reflect.DeepEqual(store.observations[0].Observation, evaluator.observation) ||
		!reflect.DeepEqual(store.observations[1].Observation, evaluator.observation) {
		t.Errorf("unavailable observations were not persisted")
	}
}

func TestCoordinatorNotifyCoalescesWakeups(t *testing.T) {
	t.Parallel()

	coordinator := newCoordinator(
		&recordingEvaluationStore{},
		testEvaluationSources(),
		map[string]definitionEvaluator{"homelab": &recordingDefinitionEvaluator{}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	coordinator.Notify()
	coordinator.Notify()

	select {
	case <-coordinator.wakeups:
	default:
		t.Fatal("Notify() did not queue a wakeup")
	}
	select {
	case <-coordinator.wakeups:
		t.Fatal("Notify() did not coalesce wakeups")
	default:
	}
}

func testEvaluationDefinition(query string) Definition {
	return Definition{Prometheus: PrometheusDefinition{
		Source: "homelab",
		Query:  query,
		Condition: Condition{
			Operator: OperatorEqual,
			Value:    0,
		},
	}}
}

func testEvaluationSources() SourceSet {
	return SourceSet{"homelab": {Prometheus: PrometheusSource{
		URL:                "https://prometheus.example.com",
		EvaluationInterval: 15 * time.Second,
		Timeout:            5 * time.Second,
		MaximumConcurrency: 1,
	}}}
}

type recordingEvaluationStore struct {
	targets      []EvaluationTarget
	observations []CurrentObservation
}

func (s *recordingEvaluationStore) ListGateEvaluationTargets(context.Context) ([]EvaluationTarget, error) {
	return slicesClone(s.targets), nil
}

func (s *recordingEvaluationStore) UpsertGateObservation(_ context.Context, observation CurrentObservation) error {
	s.observations = append(s.observations, observation)

	return nil
}

type recordingDefinitionEvaluator struct {
	observation Observation
	calls       []Definition
}

func (e *recordingDefinitionEvaluator) Evaluate(
	_ context.Context,
	definition Definition,
	_ time.Time,
) Observation {
	e.calls = append(e.calls, definition)

	return e.observation
}

func slicesClone[T any](values []T) []T {
	return append([]T(nil), values...)
}
