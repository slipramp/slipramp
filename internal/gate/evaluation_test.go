package gate

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

func TestCoordinatorReconcilesDemandAtDefinitionCadence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	definition := testEvaluationDefinition("network_active_changes")
	first := EvaluationDemand{
		RepositoryID:      uuid.New(),
		GateName:          "network-idle",
		CatalogRevisionID: uuid.New(),
		Definition:        definition,
	}
	second := EvaluationDemand{
		RepositoryID:      uuid.New(),
		GateName:          "change-window",
		CatalogRevisionID: uuid.New(),
		Definition:        definition,
	}
	store := &recordingEvaluationStore{targets: []EvaluationDemand{first, second}}
	evaluator := &recordingDefinitionEvaluator{
		observation: Observation{State: ObservationPassing, Value: new(float64)},
	}
	coordinator := testCoordinator(t, store, testEvaluationSources(), evaluator)
	processID := coordinator.processID
	coordinator.now = func() time.Time { return now }

	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("reconcile homelab error = %v", err)
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
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("reconcile homelab before interval error = %v", err)
	}
	if len(evaluator.calls) != 1 || len(store.observations) != 2 {
		t.Errorf("early reconciliation evaluated %d times and recorded %d observations",
			len(evaluator.calls), len(store.observations))
	}

	now = now.Add(time.Second)
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("reconcile homelab at interval error = %v", err)
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
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("reconcile homelab with new waiter error = %v", err)
	}
	if len(evaluator.calls) != 3 || len(store.observations) != 7 {
		t.Errorf("new waiter reconciliation evaluated %d times and recorded %d observations",
			len(evaluator.calls), len(store.observations))
	}

	// A second waiter and a review decision can change demand for the same catalog entry.
	store.targets[0].WaiterCount++
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatal(err)
	}
	store.targets[0].LastRequestedAt = now
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatal(err)
	}
	if len(evaluator.calls) != 5 {
		t.Errorf("evaluations after attachment and approval = %d, want 5", len(evaluator.calls))
	}

	store.targets = nil
	now = now.Add(time.Second)
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("reconcile homelab without waiters error = %v", err)
	}
	if len(coordinator.sources["homelab"].states) != 0 || len(evaluator.calls) != 5 {
		t.Errorf("idle coordinator states = %d, evaluations = %d", len(coordinator.sources["homelab"].states), len(evaluator.calls))
	}

	store.targets = []EvaluationDemand{first}
	now = now.Add(time.Second)
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("reconcile homelab with returning waiter error = %v", err)
	}
	if len(evaluator.calls) != 6 {
		t.Errorf("returning waiter evaluations = %d, want 6", len(evaluator.calls))
	}
}

func TestCoordinatorKeepsPollingHealthySourceWhileAnotherIsBusy(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		healthy := EvaluationDemand{
			RepositoryID:      uuid.New(),
			GateName:          "healthy",
			CatalogRevisionID: uuid.New(),
			Definition:        testEvaluationDefinition("healthy"),
		}
		slow := healthy
		slow.GateName = "slow"
		slow.Definition.Prometheus.Source = "slow"
		removed := healthy
		removed.GateName = "removed"
		removed.Definition.Prometheus.Source = "removed"
		store := &recordingEvaluationStore{targets: []EvaluationDemand{healthy, slow, removed}}

		healthyEvaluator := &recordingDefinitionEvaluator{
			observation: Observation{State: ObservationPassing, Value: new(0.0)},
		}
		slowEvaluator := &blockingDefinitionEvaluator{
			started: make(chan struct{}, 1),
			release: make(chan struct{}),
		}
		sources := testEvaluationSources()
		sources["slow"] = sources["homelab"]
		coordinator := testCoordinator(t, store, sources, healthyEvaluator)
		coordinator.sources["slow"].evaluator = slowEvaluator
		assertHealthyObservations := func(want int) {
			t.Helper()
			store.mu.Lock()
			defer store.mu.Unlock()

			if len(store.observations) != want || store.observations[want-1].GateName != healthy.GateName ||
				!store.observations[want-1].EvaluatedAt.Equal(time.Now()) {
				t.Fatalf("observations while the slow source is busy = %#v, want %d from the healthy source",
					store.observations, want)
			}
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			coordinator.Run(ctx)
		}()

		synctest.Wait()
		select {
		case <-slowEvaluator.started:
		default:
			t.Fatal("slow source did not start")
		}
		assertHealthyObservations(1)

		time.Sleep(sources["homelab"].Prometheus.EvaluationInterval)
		synctest.Wait()
		assertHealthyObservations(2)
		if len(slowEvaluator.started) != 0 {
			t.Error("a second slow query started while the first was busy")
		}

		cancel()
		<-stopped
	})
}

func TestCoordinatorBoundsEvaluationConcurrencyPerSource(t *testing.T) {
	t.Parallel()

	targets := make([]EvaluationDemand, 0, 4)
	for _, query := range []string{"first", "second", "third", "fourth"} {
		targets = append(targets, EvaluationDemand{
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
	coordinator := testCoordinator(t, store, sources, evaluator)
	var clockSeconds atomic.Int64
	coordinator.now = func() time.Time { return time.Unix(clockSeconds.Load(), 0).UTC() }

	finished := make(chan error, 1)
	go func() {
		finished <- reconcileTestSource(t.Context(), coordinator)
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
	clockSeconds.Store(60)
	close(evaluator.release)
	if err := <-finished; err != nil {
		t.Fatalf("reconcile homelab error = %v", err)
	}
	if evaluator.maximumActive != 2 {
		t.Errorf("maximum concurrent evaluations = %d, want 2", evaluator.maximumActive)
	}
	var laterQueries int
	for _, observation := range store.observations {
		if observation.EvaluatedAt.Unix() == 60 {
			laterQueries++
		}
	}
	if laterQueries != 2 {
		t.Errorf("queries timestamped after waiting for capacity = %d, want 2", laterQueries)
	}
}

func TestCoordinatorBacksOffUnavailableEvaluationsAndRecovers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	target := EvaluationDemand{
		RepositoryID:      uuid.New(),
		GateName:          "network-idle",
		CatalogRevisionID: uuid.New(),
		Definition:        testEvaluationDefinition("network_active_changes"),
	}
	store := &recordingEvaluationStore{targets: []EvaluationDemand{target}}
	zero := float64(0)
	evaluator := &sequenceDefinitionEvaluator{observations: []Observation{
		{State: ObservationUnavailable, Diagnostic: "Prometheus request timed out."},
		{State: ObservationUnavailable, Diagnostic: "Prometheus request timed out."},
		{State: ObservationPassing, Value: &zero},
	}}
	coordinator := testCoordinator(t, store, testEvaluationSources(), evaluator)
	coordinator.now = func() time.Time { return now }

	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("first reconcile homelab error = %v", err)
	}
	state := onlyEvaluationState(t, coordinator)
	if state.failures != 1 || !state.next.Equal(now.Add(15*time.Second)) {
		t.Fatalf("first unavailable state = %#v", state)
	}

	now = now.Add(15 * time.Second)
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("second reconcile homelab error = %v", err)
	}
	state = onlyEvaluationState(t, coordinator)
	if state.failures != 2 || !state.next.Equal(now.Add(30*time.Second)) {
		t.Fatalf("second unavailable state = %#v", state)
	}

	now = now.Add(29 * time.Second)
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("reconcile homelab during backoff error = %v", err)
	}
	if evaluator.calls != 2 {
		t.Fatalf("evaluations during backoff = %d, want 2", evaluator.calls)
	}

	now = now.Add(time.Second)
	if err := reconcileTestSource(t.Context(), coordinator); err != nil {
		t.Fatalf("recovery reconcile homelab error = %v", err)
	}
	state = onlyEvaluationState(t, coordinator)
	if state.failures != 0 || !state.next.Equal(now.Add(15*time.Second)) {
		t.Errorf("recovered state = %#v", state)
	}
}

func testCoordinator(t *testing.T, store EvaluationStore, sources SourceSet, evaluator definitionEvaluator) *Coordinator {
	t.Helper()
	coordinator, err := NewCoordinator(store, sources, uuid.New(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinator.sources["homelab"].evaluator = evaluator
	coordinator.jitter = func(interval time.Duration) time.Duration { return interval }
	return coordinator
}

func reconcileTestSource(ctx context.Context, coordinator *Coordinator) error {
	coordinator.refreshDemand(ctx)
	source := coordinator.sources["homelab"]
	return coordinator.reconcileSource(ctx, source, <-source.demand)
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
	if len(coordinator.sources["homelab"].states) != 1 {
		t.Fatalf("evaluation states = %d, want 1", len(coordinator.sources["homelab"].states))
	}
	for _, state := range coordinator.sources["homelab"].states {
		return state
	}

	panic("unreachable")
}

type recordingEvaluationStore struct {
	mu           sync.Mutex
	targets      []EvaluationDemand
	observations []CurrentObservation
}

func (s *recordingEvaluationStore) ListGateEvaluationDemand(context.Context) ([]EvaluationDemand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	demand := slices.Clone(s.targets)
	for i := range demand {
		digest, err := demand[i].Definition.Digest()
		if err != nil {
			return nil, err
		}
		demand[i].DefinitionDigest = digest
	}
	return demand, nil
}

func (s *recordingEvaluationStore) UpsertGateObservation(_ context.Context, observation CurrentObservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.observations = append(s.observations, observation)

	return nil
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
	ctx context.Context,
	_ Definition,
	_ time.Time,
) Observation {
	e.mu.Lock()
	e.active++
	e.maximumActive = max(e.maximumActive, e.active)
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.active--
		e.mu.Unlock()
	}()
	e.started <- struct{}{}
	select {
	case <-e.release:
	case <-ctx.Done():
		return Observation{State: ObservationUnavailable, Diagnostic: "Prometheus request was canceled."}
	}

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
