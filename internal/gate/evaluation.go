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
	Subject           *EvaluationSubject
	RepositoryName    string
	CommitSHA         string
	RepositoryID      uuid.UUID
	GateName          string
	CatalogRevisionID uuid.UUID
	Definition        Definition
	DefinitionDigest  string
	WaiterCount       int64
	LastRequestedAt   time.Time
}

// Shared evaluators reuse results for the same policy. Subject-scoped
// evaluators also include the Run or Promotion receiving the decision.
func (d EvaluationDemand) evaluationKey() string {
	if !d.Definition.Evaluator.SubjectScoped() {
		return d.DefinitionDigest
	}
	if d.Subject == nil {
		return ""
	}
	return string(d.Subject.Type) + "/" + d.Subject.ID.String() + "/" + d.DefinitionDigest
}

type EvaluationStore interface {
	// Demand is ordered by Repository, Gate name, and catalog revision.
	ListGateEvaluationDemand(context.Context) ([]EvaluationDemand, error)
	UpsertGateObservation(context.Context, CurrentObservation) error
}

type EvaluationObserver interface {
	ObserveGateEvaluation(context.Context, string, ObservationState, string, time.Duration)
}

// EvaluationNotifier wakes the evaluator after durable requirements or active
// catalog definitions change. Notifications are hints; the store remains
// authoritative and periodic reconciliation covers coalesced notifications.
type EvaluationNotifier interface {
	Notify()
}

type Evaluator interface {
	Evaluate(context.Context, EvaluationDemand, time.Time) Observation
	Close()
}

type evaluationSource struct {
	evaluatorType string
	evaluator     Evaluator
	interval      time.Duration
	slots         chan struct{}
	demand        chan []EvaluationDemand
	states        map[string]evaluationState
}

type evaluationState struct {
	demand   []EvaluationDemand
	next     time.Time
	failures int
}

type evaluationTask struct {
	key              string
	demand           []EvaluationDemand
	previousFailures int
}

type EvaluationCoordinator struct {
	store     EvaluationStore
	sources   map[string]*evaluationSource
	logger    *slog.Logger
	observer  EvaluationObserver
	processID uuid.UUID
	now       func() time.Time
	jitter    func(time.Duration) time.Duration
	wakeups   chan struct{}
}

// NewEvaluationCoordinator labels observations with this Server's processID. Dispatch
// and metrics use the same ID to reject evidence from earlier processes.
func NewEvaluationCoordinator(
	store EvaluationStore,
	sources SourceSet,
	processID uuid.UUID,
	logger *slog.Logger,
	observer EvaluationObserver,
) (*EvaluationCoordinator, error) {
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
	// An initialization error can follow successfully opened sources.
	initialized := false
	defer func() {
		if !initialized {
			for _, source := range ready {
				source.evaluator.Close()
			}
		}
	}()
	for _, name := range sources.Names() {
		config := sources[name]
		evaluatorType := config.Type()
		if _, err := lookupEvaluator(evaluatorType); err != nil {
			return nil, err
		}
		interval, concurrency := config.Schedule()
		evaluator, err := config.Open()
		if err != nil {
			return nil, fmt.Errorf("initialize Gate source %q: %w", name, err)
		}
		ready[name] = &evaluationSource{
			evaluatorType: evaluatorType,
			evaluator:     evaluator,
			interval:      interval,
			slots:         make(chan struct{}, concurrency),
			demand:        make(chan []EvaluationDemand, 1),
			states:        make(map[string]evaluationState),
		}
	}

	initialized = true
	return &EvaluationCoordinator{
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

func (c *EvaluationCoordinator) Run(ctx context.Context) {
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

func (c *EvaluationCoordinator) Notify() {
	select {
	case c.wakeups <- struct{}{}:
	default:
	}
}

func (c *EvaluationCoordinator) refreshDemand(ctx context.Context) {
	demand, err := c.store.ListGateEvaluationDemand(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Warn("list Gate evaluation demand", "error", err)
		}
		return
	}

	bySource := make(map[string][]EvaluationDemand, len(c.sources))
	for _, item := range demand {
		policy := item.Definition.Evaluator
		if policy == nil {
			continue
		}
		name := policy.SourceName()
		// The active catalog can outlive a source removed at Server restart.
		if source, configured := c.sources[name]; configured && source.evaluatorType == policy.Type() {
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

func (c *EvaluationCoordinator) runSource(ctx context.Context, name string, source *evaluationSource) {
	defer source.evaluator.Close()
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

func (c *EvaluationCoordinator) reconcileSource(ctx context.Context, source *evaluationSource, demand []EvaluationDemand) error {
	demandByKey := make(map[string][]EvaluationDemand)
	for _, item := range demand {
		key := item.evaluationKey()
		demandByKey[key] = append(demandByKey[key], item)
	}

	now := c.now().UTC()
	activeStates := make(map[string]evaluationState, len(demandByKey))
	tasks := make([]evaluationTask, 0, len(demandByKey))
	keys := slices.Sorted(maps.Keys(demandByKey))
	for _, key := range keys {
		requests := demandByKey[key]
		state, found := source.states[key]
		if found && slices.EqualFunc(state.demand, requests, sameDemand) && now.Before(state.next) {
			activeStates[key] = state
			continue
		}

		tasks = append(tasks, evaluationTask{
			key:              key,
			demand:           requests,
			previousFailures: state.failures,
		})
	}

	results, err := c.evaluate(ctx, source, tasks)
	if err != nil {
		return err
	}
	for index, state := range results {
		activeStates[tasks[index].key] = state
	}

	source.states = activeStates

	return nil
}

func sameDemand(a, b EvaluationDemand) bool {
	return a.RepositoryID == b.RepositoryID && a.GateName == b.GateName &&
		a.CatalogRevisionID == b.CatalogRevisionID && a.DefinitionDigest == b.DefinitionDigest &&
		a.WaiterCount == b.WaiterCount && a.LastRequestedAt.Equal(b.LastRequestedAt)
}

func (c *EvaluationCoordinator) evaluate(
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

func (c *EvaluationCoordinator) evaluateDefinition(
	ctx context.Context,
	source *evaluationSource,
	task evaluationTask,
) (evaluationState, error) {
	startedAt := time.Now()
	now := c.now().UTC()
	observation := source.evaluator.Evaluate(ctx, task.demand[0], now)
	if err := observation.Validate(); err != nil ||
		(observation.Evidence != nil && observation.Evidence.Payload.Type() != source.evaluatorType) {
		observation = Observation{
			State: ObservationUnavailable, Diagnostic: "The evaluator returned invalid evidence.",
		}
	}
	if c.observer != nil {
		c.observer.ObserveGateEvaluation(
			ctx, source.evaluatorType, observation.State, observation.FailureClass, time.Since(startedAt),
		)
	}

	validUntil := now.Add(observationValidityIntervals * source.interval)
	for _, item := range task.demand {
		record := CurrentObservation{
			RepositoryID:       item.RepositoryID,
			GateName:           item.GateName,
			CatalogRevisionID:  item.CatalogRevisionID,
			DefinitionDigest:   item.DefinitionDigest,
			EvaluatorProcessID: c.processID,
			Observation:        observation,
			EvaluatedAt:        now,
			ValidUntil:         validUntil,
		}
		if item.Subject != nil {
			record.SubjectType = item.Subject.Type
			record.SubjectID = item.Subject.ID
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
