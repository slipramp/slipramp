package gate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	DefaultReconciliationInterval   = time.Second
	MaximumEvaluationFailureBackoff = time.Minute
	observationValidityIntervals    = 3
)

// EvaluationDemand identifies an active catalog definition with unreleased requirements.
// The catalog digest lets several attachments share one Prometheus query.
type EvaluationDemand struct {
	RepositoryID      uuid.UUID
	GateName          string
	CatalogRevisionID uuid.UUID
	Definition        Definition
	DefinitionDigest  string
	WaiterCount       int64
	LastRequestedAt   time.Time
}

type EvaluationStore interface {
	// Demand is ordered by Repository, Gate name, and catalog revision.
	ListGateEvaluationDemand(context.Context) ([]EvaluationDemand, error)
	UpsertGateObservation(context.Context, CurrentObservation) error
}

type EvaluationObserver interface {
	ObserveGateEvaluation(context.Context, ObservationState, time.Duration)
}

// EvaluationNotifier wakes the evaluator after durable requirements or active
// catalog definitions change. Notifications are hints; the store remains
// authoritative and periodic reconciliation covers coalesced notifications.
type EvaluationNotifier interface {
	Notify()
}

type definitionEvaluator interface {
	Evaluate(context.Context, Definition, time.Time) Observation
}

type evaluationSource struct {
	evaluator definitionEvaluator
	interval  time.Duration
	slots     chan struct{}
	demand    chan []EvaluationDemand
	states    map[string]evaluationState
}

type evaluationState struct {
	demand   []EvaluationDemand
	next     time.Time
	failures int
}

type evaluationTask struct {
	digest           string
	demand           []EvaluationDemand
	previousFailures int
}

type Coordinator struct {
	store     EvaluationStore
	sources   map[string]*evaluationSource
	logger    *slog.Logger
	observer  EvaluationObserver
	processID uuid.UUID
	now       func() time.Time
	jitter    func(time.Duration) time.Duration
	wakeups   chan struct{}
}

// NewCoordinator labels observations with this Server's processID. Dispatch
// and metrics use the same ID to reject evidence from earlier processes.
func NewCoordinator(
	store EvaluationStore,
	sources SourceSet,
	processID uuid.UUID,
	logger *slog.Logger,
	observer EvaluationObserver,
) (*Coordinator, error) {
	if store == nil {
		return nil, errors.New("Gate evaluation store is required") //nolint:staticcheck // Gate is a domain name.
	}
	if logger == nil {
		return nil, errors.New("Gate evaluation logger is required") //nolint:staticcheck // Gate is a domain name.
	}
	if processID == uuid.Nil {
		return nil, errors.New("Gate evaluator process ID is required") //nolint:staticcheck // Gate is a domain name.
	}
	if err := sources.Validate(); err != nil {
		return nil, fmt.Errorf("validate Gate evaluation sources: %w", err)
	}

	ready := make(map[string]*evaluationSource, len(sources))
	for _, name := range sources.Names() {
		config := sources[name].Prometheus
		evaluator, err := NewPrometheusEvaluator(config)
		if err != nil {
			return nil, fmt.Errorf("initialize Gate source %q: %w", name, err)
		}
		ready[name] = &evaluationSource{
			evaluator: evaluator,
			interval:  config.EvaluationInterval,
			slots:     make(chan struct{}, config.MaximumConcurrency),
			demand:    make(chan []EvaluationDemand, 1),
			states:    make(map[string]evaluationState),
		}
	}

	return &Coordinator{
		store:     store,
		sources:   ready,
		logger:    logger.With("component", "gate_evaluator"),
		observer:  observer,
		processID: processID,
		now:       time.Now,
		jitter: func(interval time.Duration) time.Duration {
			return time.Duration(float64(interval) * (0.9 + rand.Float64()*0.2))
		},
		wakeups: make(chan struct{}, 1),
	}, nil
}

func (c *Coordinator) Run(ctx context.Context) {
	var workers sync.WaitGroup
	for name, source := range c.sources {
		workers.Go(func() { c.runSource(ctx, name, source) })
	}
	defer workers.Wait()

	c.refreshDemand(ctx)
	ticker := time.NewTicker(DefaultReconciliationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshDemand(ctx)
		case <-c.wakeups:
			c.refreshDemand(ctx)
		}
	}
}

func (c *Coordinator) Notify() {
	select {
	case c.wakeups <- struct{}{}:
	default:
	}
}

func (c *Coordinator) refreshDemand(ctx context.Context) {
	demand, err := c.store.ListGateEvaluationDemand(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Warn("list Gate evaluation demand", "error", err)
		}
		return
	}

	bySource := make(map[string][]EvaluationDemand, len(c.sources))
	for _, item := range demand {
		name := item.Definition.Prometheus.Source
		// The active catalog can outlive a source removed at Server restart.
		if _, configured := c.sources[name]; configured {
			bySource[name] = append(bySource[name], item)
		}
	}
	for name, source := range c.sources {
		// Keep only the latest demand while this source is busy. Run is
		// the sole sender and can publish without waiting for any worker.
		select {
		case <-source.demand:
		default:
		}
		source.demand <- bySource[name]
	}
}

func (c *Coordinator) runSource(ctx context.Context, name string, source *evaluationSource) {
	for {
		select {
		case <-ctx.Done():
			return
		case demand := <-source.demand:
			if err := c.reconcileSource(ctx, source, demand); err != nil && ctx.Err() == nil {
				c.logger.Warn("evaluate repository Gates", "source", name, "error", err)
			}
		}
	}
}

func (c *Coordinator) reconcileSource(ctx context.Context, source *evaluationSource, demand []EvaluationDemand) error {
	demandByDigest := make(map[string][]EvaluationDemand)
	for _, item := range demand {
		demandByDigest[item.DefinitionDigest] = append(demandByDigest[item.DefinitionDigest], item)
	}

	now := c.now().UTC()
	activeStates := make(map[string]evaluationState, len(demandByDigest))
	tasks := make([]evaluationTask, 0, len(demandByDigest))
	digests := slices.Sorted(maps.Keys(demandByDigest))
	for _, digest := range digests {
		requests := demandByDigest[digest]
		state, found := source.states[digest]
		if found && slices.Equal(state.demand, requests) && now.Before(state.next) {
			activeStates[digest] = state
			continue
		}

		tasks = append(tasks, evaluationTask{
			digest:           digest,
			demand:           requests,
			previousFailures: state.failures,
		})
	}

	results, err := c.evaluate(ctx, source, tasks)
	if err != nil {
		return err
	}
	for index, state := range results {
		activeStates[tasks[index].digest] = state
	}

	source.states = activeStates

	return nil
}

func (c *Coordinator) evaluate(
	ctx context.Context,
	source *evaluationSource,
	tasks []evaluationTask,
) ([]evaluationState, error) {
	results := make([]evaluationState, len(tasks))
	group, groupContext := errgroup.WithContext(ctx)

schedule:
	for index, task := range tasks {
		select {
		case source.slots <- struct{}{}:
		case <-groupContext.Done():
			break schedule
		}

		group.Go(func() error {
			defer func() { <-source.slots }()
			state, err := c.evaluateDefinition(groupContext, source, task)
			results[index] = state
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	return results, ctx.Err()
}

func (c *Coordinator) evaluateDefinition(
	ctx context.Context,
	source *evaluationSource,
	task evaluationTask,
) (evaluationState, error) {
	startedAt := time.Now()
	now := c.now().UTC()
	observation := source.evaluator.Evaluate(ctx, task.demand[0].Definition, now)
	if err := observation.Validate(); err != nil {
		return evaluationState{}, fmt.Errorf("validate Gate evaluation result: %w", err)
	}
	if c.observer != nil {
		c.observer.ObserveGateEvaluation(ctx, observation.State, time.Since(startedAt))
	}

	validUntil := now.Add(observationValidityIntervals * source.interval)
	for _, item := range task.demand {
		record := CurrentObservation{
			RepositoryID:       item.RepositoryID,
			GateName:           item.GateName,
			CatalogRevisionID:  item.CatalogRevisionID,
			DefinitionDigest:   task.digest,
			EvaluatorProcessID: c.processID,
			Observation:        observation,
			EvaluatedAt:        now,
			ValidUntil:         validUntil,
		}
		if err := c.store.UpsertGateObservation(ctx, record); err != nil {
			return evaluationState{}, fmt.Errorf("record Gate observation for %q: %w", item.GateName, err)
		}
	}

	failures := 0
	if observation.State == ObservationUnavailable {
		failures = task.previousFailures + 1
	}
	delay := source.interval
	if failures > 0 {
		delay = evaluationFailureBackoff(delay, failures)
	}
	delay = c.jitter(delay)
	if failures > 0 {
		delay = min(delay, max(source.interval, MaximumEvaluationFailureBackoff))
	}
	return evaluationState{
		demand:   task.demand,
		next:     now.Add(delay),
		failures: failures,
	}, nil
}

func evaluationFailureBackoff(interval time.Duration, failures int) time.Duration {
	backoff := interval
	maximum := max(interval, MaximumEvaluationFailureBackoff)
	for range max(failures-1, 0) {
		if backoff >= maximum/2 {
			return maximum
		}
		backoff *= 2
	}

	return min(backoff, maximum)
}
