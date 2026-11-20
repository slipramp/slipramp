package gate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	DefaultReconciliationInterval = time.Second
	observationValidityIntervals  = 3
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
}

type Coordinator struct {
	store                  EvaluationStore
	sources                SourceSet
	evaluators             map[string]definitionEvaluator
	reconciliationInterval time.Duration
	logger                 *slog.Logger
	processID              uuid.UUID
	now                    func() time.Time
	wakeups                chan struct{}
	states                 map[string]evaluationState
}

func NewCoordinator(
	store EvaluationStore,
	sources SourceSet,
	logger *slog.Logger,
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

	return newCoordinator(store, sources, evaluators, logger), nil
}

func newCoordinator(
	store EvaluationStore,
	sources SourceSet,
	evaluators map[string]definitionEvaluator,
	logger *slog.Logger,
) *Coordinator {
	return &Coordinator{
		store:                  store,
		sources:                sources,
		evaluators:             evaluators,
		reconciliationInterval: DefaultReconciliationInterval,
		logger:                 logger.With("component", "gate_evaluator"),
		processID:              uuid.New(),
		now:                    time.Now,
		wakeups:                make(chan struct{}, 1),
		states:                 map[string]evaluationState{},
	}
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

		observation := evaluator.Evaluate(ctx, group.definition, now)
		if err := observation.Validate(); err != nil {
			return fmt.Errorf("validate Gate evaluation result: %w", err)
		}
		validUntil := now.Add(observationValidityIntervals * source.Prometheus.EvaluationInterval)
		for _, target := range group.targets {
			record := CurrentObservation{
				RepositoryID:       target.RepositoryID,
				GateName:           target.GateName,
				CatalogRevisionID:  target.CatalogRevisionID,
				DefinitionDigest:   digest,
				EvaluatorProcessID: c.processID,
				Observation:        observation,
				EvaluatedAt:        now,
				ValidUntil:         validUntil,
			}
			if err := c.store.UpsertGateObservation(ctx, record); err != nil {
				return fmt.Errorf("record Gate observation for %q: %w", target.GateName, err)
			}
		}

		activeStates[digest] = evaluationState{
			membership: membership,
			next:       now.Add(source.Prometheus.EvaluationInterval),
		}
	}

	c.states = activeStates

	return nil
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
