package gate

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"sync"
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
	coordinator.jitter = func(interval time.Duration) time.Duration { return interval }

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
	coordinator.jitter = func(interval time.Duration) time.Duration { return interval }

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

func TestCoordinatorBoundsEvaluationConcurrencyPerSource(t *testing.T) {
	t.Parallel()

	targets := make([]EvaluationTarget, 0, 4)
	for _, query := range []string{"first", "second", "third", "fourth"} {
		targets = append(targets, EvaluationTarget{
			RepositoryID:      uuid.New(),
			GateName:          query,
			CatalogRevisionID: uuid.New(),
			Definition:        testEvaluationDefinition(query),
		})
	}
	store := &recordingEvaluationStore{targets: targets}
	evaluator := &blockingDefinitionEvaluator{
		started: make(chan struct{}, len(targets)),
		release: make(chan struct{}),
	}
	sources := testEvaluationSources()
	source := sources["homelab"]
	source.Prometheus.MaximumConcurrency = 2
	sources["homelab"] = source
	coordinator := newCoordinator(
		store,
		sources,
		map[string]definitionEvaluator{"homelab": evaluator},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	coordinator.jitter = func(interval time.Duration) time.Duration { return interval }

	finished := make(chan error, 1)
	go func() {
		finished <- coordinator.Reconcile(t.Context())
	}()

	for range 2 {
		select {
		case <-evaluator.started:
		case <-time.After(time.Second):
			t.Fatal("two source evaluations did not start concurrently")
		}
	}
	select {
	case <-evaluator.started:
		t.Fatal("source concurrency limit was exceeded")
	case <-time.After(25 * time.Millisecond):
	}
	close(evaluator.release)
	if err := <-finished; err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if evaluator.maximumActive != 2 {
		t.Errorf("maximum concurrent evaluations = %d, want 2", evaluator.maximumActive)
	}
}

func TestCoordinatorBacksOffUnavailableEvaluationsAndRecovers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	target := EvaluationTarget{
		RepositoryID:      uuid.New(),
		GateName:          "network-idle",
		CatalogRevisionID: uuid.New(),
		Definition:        testEvaluationDefinition("network_active_changes"),
	}
	store := &recordingEvaluationStore{targets: []EvaluationTarget{target}}
	zero := float64(0)
	evaluator := &sequenceDefinitionEvaluator{observations: []Observation{
		{State: ObservationUnavailable, Diagnostic: "Prometheus request timed out."},
		{State: ObservationUnavailable, Diagnostic: "Prometheus request timed out."},
		{State: ObservationPassing, Value: &zero},
	}}
	coordinator := newCoordinator(
		store,
		testEvaluationSources(),
		map[string]definitionEvaluator{"homelab": evaluator},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	coordinator.now = func() time.Time { return now }
	coordinator.jitter = func(interval time.Duration) time.Duration { return interval }

	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	state := onlyEvaluationState(t, coordinator)
	if state.failures != 1 || !state.next.Equal(now.Add(15*time.Second)) {
		t.Fatalf("first unavailable state = %#v", state)
	}

	now = now.Add(15 * time.Second)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	state = onlyEvaluationState(t, coordinator)
	if state.failures != 2 || !state.next.Equal(now.Add(30*time.Second)) {
		t.Fatalf("second unavailable state = %#v", state)
	}

	now = now.Add(29 * time.Second)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() during backoff error = %v", err)
	}
	if evaluator.calls != 2 {
		t.Fatalf("evaluations during backoff = %d, want 2", evaluator.calls)
	}

	now = now.Add(time.Second)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("recovery Reconcile() error = %v", err)
	}
	state = onlyEvaluationState(t, coordinator)
	if state.failures != 0 || !state.next.Equal(now.Add(15*time.Second)) {
		t.Errorf("recovered state = %#v", state)
	}
}

func TestCoordinatorCleansInactiveObservationsOnMaintenanceCadence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	store := &recordingEvaluationStore{}
	coordinator := newCoordinator(
		store,
		testEvaluationSources(),
		map[string]definitionEvaluator{"homelab": &recordingDefinitionEvaluator{}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	coordinator.now = func() time.Time { return now }

	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("initial Reconcile() error = %v", err)
	}
	if !reflect.DeepEqual(store.cleanups, []time.Time{now.Add(-24 * time.Hour)}) {
		t.Fatalf("initial cleanup cutoffs = %v", store.cleanups)
	}

	now = now.Add(59 * time.Minute)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("early Reconcile() error = %v", err)
	}
	if len(store.cleanups) != 1 {
		t.Fatalf("early cleanup calls = %d, want 1", len(store.cleanups))
	}

	now = now.Add(time.Minute)
	if err := coordinator.Reconcile(t.Context()); err != nil {
		t.Fatalf("due Reconcile() error = %v", err)
	}
	if !reflect.DeepEqual(store.cleanups, []time.Time{
		now.Add(-25 * time.Hour),
		now.Add(-24 * time.Hour),
	}) {
		t.Errorf("cleanup cutoffs = %v", store.cleanups)
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

func onlyEvaluationState(t *testing.T, coordinator *Coordinator) evaluationState {
	t.Helper()
	if len(coordinator.states) != 1 {
		t.Fatalf("evaluation states = %d, want 1", len(coordinator.states))
	}
	for _, state := range coordinator.states {
		return state
	}

	panic("unreachable")
}

type recordingEvaluationStore struct {
	mu           sync.Mutex
	targets      []EvaluationTarget
	observations []CurrentObservation
	cleanups     []time.Time
}

func (s *recordingEvaluationStore) ListGateEvaluationTargets(context.Context) ([]EvaluationTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slicesClone(s.targets), nil
}

func (s *recordingEvaluationStore) UpsertGateObservation(_ context.Context, observation CurrentObservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.observations = append(s.observations, observation)

	return nil
}

func (s *recordingEvaluationStore) DeleteInactiveGateObservations(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanups = append(s.cleanups, before)

	return 0, nil
}

type recordingDefinitionEvaluator struct {
	mu          sync.Mutex
	observation Observation
	calls       []Definition
}

type blockingDefinitionEvaluator struct {
	mu            sync.Mutex
	started       chan struct{}
	release       chan struct{}
	active        int
	maximumActive int
}

func (e *blockingDefinitionEvaluator) Evaluate(
	_ context.Context,
	_ Definition,
	_ time.Time,
) Observation {
	e.mu.Lock()
	e.active++
	e.maximumActive = max(e.maximumActive, e.active)
	e.mu.Unlock()
	e.started <- struct{}{}
	<-e.release
	e.mu.Lock()
	e.active--
	e.mu.Unlock()

	value := float64(0)

	return Observation{State: ObservationPassing, Value: &value}
}

type sequenceDefinitionEvaluator struct {
	observations []Observation
	calls        int
}

func (e *sequenceDefinitionEvaluator) Evaluate(
	_ context.Context,
	_ Definition,
	_ time.Time,
) Observation {
	observation := e.observations[e.calls]
	e.calls++

	return observation
}

func (e *recordingDefinitionEvaluator) Evaluate(
	_ context.Context,
	definition Definition,
	_ time.Time,
) Observation {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.calls = append(e.calls, definition)

	return e.observation
}

func slicesClone[T any](values []T) []T {
	return append([]T(nil), values...)
}
