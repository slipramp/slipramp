package gate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	DefaultReconciliationInterval       = time.Second
	DefaultObservationCleanupInterval   = time.Hour
	DefaultInactiveObservationRetention = 24 * time.Hour
	MaximumEvaluationFailureBackoff     = time.Minute
	observationValidityIntervals        = 3
)

// EvaluationTarget identifies one active Gate definition required by at
// least one unreleased subject. Several targets may share one normalized
// definition and therefore one Prometheus query.
type EvaluationTarget struct {
	RepositoryID      uuid.UUID
	GateName          string
	CatalogRevisionID uuid.UUID
	Definition        Definition
}

func (t EvaluationTarget) Validate() error {
	if t.RepositoryID == uuid.Nil {
		return errors.New("Gate evaluation Repository ID is required") //nolint:staticcheck // Gate is a domain name.
	}
	if err := kubevalidate.DNSLabel("Gate evaluation name", t.GateName); err != nil {
		return err
	}
	if t.CatalogRevisionID == uuid.Nil {
		return errors.New("Gate evaluation catalog revision ID is required") //nolint:staticcheck // Gate is a domain name.
	}
	if err := t.Definition.Validate(); err != nil {
		return fmt.Errorf("validate Gate evaluation definition: %w", err)
	}

	return nil
}

type EvaluationStore interface {
	ListGateEvaluationTargets(context.Context) ([]EvaluationTarget, error)
	UpsertGateObservation(context.Context, CurrentObservation) error
	DeleteInactiveGateObservations(context.Context, time.Time) (int64, error)
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

type evaluationGroup struct {
	definition Definition
	targets    []EvaluationTarget
}

type evaluationState struct {
	membership string
	next       time.Time
	failures   int
}

type evaluationTask struct {
	digest           string
	group            evaluationGroup
	membership       string
	source           Source
	evaluator        definitionEvaluator
	previousFailures int
}

type evaluationResult struct {
	digest string
	state  evaluationState
}

type CoordinatorOption func(*Coordinator)

func WithEvaluationObserver(observer EvaluationObserver) CoordinatorOption {
	return func(coordinator *Coordinator) {
		coordinator.observer = observer
	}
}

type Coordinator struct {
	store                  EvaluationStore
	sources                SourceSet
	evaluators             map[string]definitionEvaluator
	reconciliationInterval time.Duration
	logger                 *slog.Logger
	observer               EvaluationObserver
	processID              uuid.UUID
	now                    func() time.Time
	jitter                 func(time.Duration) time.Duration
	wakeups                chan struct{}
	states                 map[string]evaluationState
	cleanupInterval        time.Duration
	inactiveRetention      time.Duration
	nextCleanup            time.Time
}

func NewCoordinator(
	store EvaluationStore,
	sources SourceSet,
	logger *slog.Logger,
	options ...CoordinatorOption,
) (*Coordinator, error) {
	if store == nil {
		return nil, errors.New("Gate evaluation store is required") //nolint:staticcheck // Gate is a domain name.
	}
	if logger == nil {
		return nil, errors.New("Gate evaluation logger is required") //nolint:staticcheck // Gate is a domain name.
	}
	if err := sources.Validate(); err != nil {
		return nil, fmt.Errorf("validate Gate evaluation sources: %w", err)
	}

	evaluators := make(map[string]definitionEvaluator, len(sources))
	for _, name := range sources.Names() {
		evaluator, err := NewPrometheusEvaluator(sources[name].Prometheus)
		if err != nil {
			return nil, fmt.Errorf("initialize Gate source %q: %w", name, err)
		}
		evaluators[name] = evaluator
	}

	return newCoordinator(store, sources, evaluators, logger, options...), nil
}

func newCoordinator(
	store EvaluationStore,
	sources SourceSet,
	evaluators map[string]definitionEvaluator,
	logger *slog.Logger,
	options ...CoordinatorOption,
) *Coordinator {
	coordinator := &Coordinator{
		store:                  store,
		sources:                sources,
		evaluators:             evaluators,
		reconciliationInterval: DefaultReconciliationInterval,
		logger:                 logger.With("component", "gate_evaluator"),
		processID:              uuid.New(),
		now:                    time.Now,
		jitter: func(interval time.Duration) time.Duration {
			return time.Duration(float64(interval) * (0.9 + rand.Float64()*0.2))
		},
		wakeups:           make(chan struct{}, 1),
		states:            map[string]evaluationState{},
		cleanupInterval:   DefaultObservationCleanupInterval,
		inactiveRetention: DefaultInactiveObservationRetention,
	}
	for _, option := range options {
		option(coordinator)
	}

	return coordinator
}

func (c *Coordinator) Run(ctx context.Context) {
	c.reconcileAndLog(ctx)
	ticker := time.NewTicker(c.reconciliationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.reconcileAndLog(ctx)
		case <-c.wakeups:
			c.reconcileAndLog(ctx)
		}
	}
}

func (c *Coordinator) Notify() {
	select {
	case c.wakeups <- struct{}{}:
	default:
	}
}

// ProcessID identifies observations produced by this Server process. Release
// rejects observations from an earlier process so endpoint or credential
// changes cannot authorize new work after restart.
func (c *Coordinator) ProcessID() uuid.UUID {
	return c.processID
}

func (c *Coordinator) reconcileAndLog(ctx context.Context) {
	if err := c.Reconcile(ctx); err != nil && ctx.Err() == nil {
		c.logger.Warn("evaluate repository Gates", "error", err)
	}
}

// Reconcile evaluates definitions that are new, changed, or due at one
// injected-clock instant. It is exported so deterministic tests do not need
// wall-clock sleeps.
func (c *Coordinator) Reconcile(ctx context.Context) error {
	targets, err := c.store.ListGateEvaluationTargets(ctx)
	if err != nil {
		return fmt.Errorf("list Gate evaluation targets: %w", err)
	}

	groups, err := groupEvaluationTargets(targets)
	if err != nil {
		return err
	}

	now := c.now().UTC()
	activeStates := make(map[string]evaluationState, len(groups))
	tasks := make([]evaluationTask, 0, len(groups))
	digests := slices.Sorted(maps.Keys(groups))
	for _, digest := range digests {
		group := groups[digest]
		membership := evaluationMembership(group.targets)
		state, found := c.states[digest]
		if found && state.membership == membership && now.Before(state.next) {
			activeStates[digest] = state
			continue
		}

		source, found := c.sources.Lookup(group.definition.Prometheus.Source)
		if !found {
			return fmt.Errorf( //nolint:staticcheck // Gate is a domain name.
				"Gate definition selects unknown source %q",
				group.definition.Prometheus.Source,
			)
		}
		evaluator := c.evaluators[group.definition.Prometheus.Source]
		if evaluator == nil {
			return fmt.Errorf( //nolint:staticcheck // Gate is a domain name.
				"Gate source %q has no evaluator",
				group.definition.Prometheus.Source,
			)
		}

		tasks = append(tasks, evaluationTask{
			digest:           digest,
			group:            group,
			membership:       membership,
			source:           source,
			evaluator:        evaluator,
			previousFailures: state.failures,
		})
	}

	results, err := c.evaluate(ctx, tasks, now)
	if err != nil {
		return err
	}
	for _, result := range results {
		activeStates[result.digest] = result.state
	}

	c.states = activeStates
	if !now.Before(c.nextCleanup) {
		if _, err := c.store.DeleteInactiveGateObservations(ctx, now.Add(-c.inactiveRetention)); err != nil {
			return fmt.Errorf("delete inactive Gate observations: %w", err)
		}
		c.nextCleanup = now.Add(c.cleanupInterval)
	}

	return nil
}

func (c *Coordinator) evaluate(
	ctx context.Context,
	tasks []evaluationTask,
	now time.Time,
) ([]evaluationResult, error) {
	results := make([]evaluationResult, len(tasks))
	semaphores := make(map[string]chan struct{}, len(c.sources))
	for name, source := range c.sources {
		semaphores[name] = make(chan struct{}, source.Prometheus.MaximumConcurrency)
	}

	group, groupContext := errgroup.WithContext(ctx)
	for index, task := range tasks {
		group.Go(func() error {
			semaphore := semaphores[task.group.definition.Prometheus.Source]
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-groupContext.Done():
				return groupContext.Err()
			}

			startedAt := time.Now()
			observation := task.evaluator.Evaluate(groupContext, task.group.definition, now)
			if err := observation.Validate(); err != nil {
				return fmt.Errorf("validate Gate evaluation result: %w", err)
			}
			if c.observer != nil {
				c.observer.ObserveGateEvaluation(ctx, observation.State, time.Since(startedAt))
			}

			validUntil := now.Add(observationValidityIntervals * task.source.Prometheus.EvaluationInterval)
			for _, target := range task.group.targets {
				record := CurrentObservation{
					RepositoryID:       target.RepositoryID,
					GateName:           target.GateName,
					CatalogRevisionID:  target.CatalogRevisionID,
					DefinitionDigest:   task.digest,
					EvaluatorProcessID: c.processID,
					Observation:        observation,
					EvaluatedAt:        now,
					ValidUntil:         validUntil,
				}
				if err := c.store.UpsertGateObservation(groupContext, record); err != nil {
					return fmt.Errorf("record Gate observation for %q: %w", target.GateName, err)
				}
			}

			failures := 0
			if observation.State == ObservationUnavailable {
				failures = task.previousFailures + 1
			}
			delay := task.source.Prometheus.EvaluationInterval
			if failures > 0 {
				delay = evaluationFailureBackoff(delay, failures)
			}
			delay = c.jitter(delay)
			if failures > 0 {
				delay = min(delay, max(task.source.Prometheus.EvaluationInterval, MaximumEvaluationFailureBackoff))
			}
			results[index] = evaluationResult{
				digest: task.digest,
				state: evaluationState{
					membership: task.membership,
					next:       now.Add(delay),
					failures:   failures,
				},
			}

			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	return results, nil
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

func groupEvaluationTargets(targets []EvaluationTarget) (map[string]evaluationGroup, error) {
	groups := make(map[string]evaluationGroup)
	for _, target := range targets {
		if err := target.Validate(); err != nil {
			return nil, err
		}
		digest, err := target.Definition.Digest()
		if err != nil {
			return nil, fmt.Errorf("digest Gate evaluation definition: %w", err)
		}

		group := groups[digest]
		group.definition = target.Definition
		group.targets = append(group.targets, target)
		groups[digest] = group
	}

	return groups, nil
}

func evaluationMembership(targets []EvaluationTarget) string {
	keys := make([]string, 0, len(targets))
	for _, target := range targets {
		keys = append(keys, strings.Join([]string{
			target.RepositoryID.String(),
			target.GateName,
			target.CatalogRevisionID.String(),
		}, "/"))
	}

	slices.Sort(keys)

	return strings.Join(keys, "\n")
}
