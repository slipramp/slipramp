// Package scheduletrigger evaluates repository-defined UTC cron schedules and
// creates ordinary immutable Runs.
package scheduletrigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"k8s.io/utils/clock"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cronexpr"
	"github.com/slipramp/slipramp/internal/run"
)

const DefaultEvaluationInterval = 5 * time.Second

type Catalog interface {
	ListTargets(context.Context, catalog.TargetListFilter) (catalog.TargetList, error)
}

type RunCreator interface {
	CreateRun(context.Context, run.CreateRequest) (run.Run, error)
}

type Scheduler struct {
	catalog  Catalog
	runs     RunCreator
	interval time.Duration
	logger   *slog.Logger
	clock    clock.WithTicker
	states   map[scheduleKey]scheduleState
}

type scheduleKey struct {
	targetID    uuid.UUID
	actionName  string
	triggerName string
}

type scheduleDefinition struct {
	key               scheduleKey
	catalogRevisionID uuid.UUID
	cron              string
	parameters        map[string]any
}

type scheduleState struct {
	definition scheduleDefinition
	schedule   cronexpr.Schedule
	next       time.Time
}

type Option func(*Scheduler)

func WithClock(value clock.WithTicker) Option {
	return func(scheduler *Scheduler) {
		if value != nil {
			scheduler.clock = value
		}
	}
}

func NewScheduler(
	catalogs Catalog,
	runs RunCreator,
	interval time.Duration,
	logger *slog.Logger,
	options ...Option,
) *Scheduler {
	scheduler := &Scheduler{
		catalog:  catalogs,
		runs:     runs,
		interval: interval,
		logger:   logger.With("component", "schedule_trigger_scheduler"),
		clock:    clock.RealClock{},
		states:   map[scheduleKey]scheduleState{},
	}
	for _, option := range options {
		option(scheduler)
	}

	return scheduler
}

func (s *Scheduler) Run(ctx context.Context) {
	s.evaluateAndLog(ctx)
	ticker := s.clock.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			s.evaluateAndLog(ctx)
		}
	}
}

func (s *Scheduler) evaluateAndLog(ctx context.Context) {
	if err := s.Evaluate(ctx); err != nil && ctx.Err() == nil {
		s.logger.Warn("evaluate scheduled Action triggers", "error", creationDiagnostic(err))
	}
}

// Evaluate reconciles current catalog schedules at one injected-clock instant.
// It is exported so deterministic tests do not need wall-clock sleeps.
func (s *Scheduler) Evaluate(ctx context.Context) error {
	now := s.clock.Now().UTC()
	definitions, err := s.listDefinitions(ctx)
	if err != nil {
		return err
	}

	seen := make(map[scheduleKey]struct{}, len(definitions))
	for _, definition := range definitions {
		seen[definition.key] = struct{}{}
		state, found := s.states[definition.key]
		if !found || state.definition.catalogRevisionID != definition.catalogRevisionID ||
			state.definition.cron != definition.cron {
			parsed, err := cronexpr.Parse(definition.cron)
			if err != nil {
				return fmt.Errorf("parse active scheduled trigger: %w", err)
			}
			s.states[definition.key] = scheduleState{
				definition: definition,
				schedule:   parsed,
				next:       parsed.Next(now),
			}
			continue
		}

		state.definition = definition
		if state.next.After(now) {
			s.states[definition.key] = state
			continue
		}

		s.createOccurrence(ctx, definition, state.next)
		state.next = state.schedule.Next(now)
		s.states[definition.key] = state
	}

	for key := range s.states {
		if _, found := seen[key]; !found {
			delete(s.states, key)
		}
	}

	return nil
}

func (s *Scheduler) createOccurrence(
	ctx context.Context,
	definition scheduleDefinition,
	scheduledAt time.Time,
) {
	idempotencyKey := run.ScheduleOccurrenceIdempotencyKey(
		definition.key.targetID,
		definition.key.actionName,
		definition.key.triggerName,
		scheduledAt,
	)
	created, err := s.runs.CreateRun(ctx, run.CreateRequest{
		TargetID:   definition.key.targetID,
		ActionName: definition.key.actionName,
		Parameters: maps.Clone(definition.parameters),
		Inputs:     map[string]uuid.UUID{},
		Source: run.Source{
			Type: run.SourceTypeSchedule,
			Schedule: &run.ScheduleRunSource{
				TriggerName: definition.key.triggerName,
				ScheduledAt: scheduledAt,
			},
		},
		IdempotencyKey: &idempotencyKey,
	})
	if errors.Is(err, run.ErrScheduleOverlap) {
		s.logger.Info(
			"scheduled Action occurrence skipped because its previous Run is unfinished",
			"target", definition.key.targetID,
			"action", definition.key.actionName,
			"trigger", definition.key.triggerName,
			"scheduled_at", scheduledAt,
		)
		return
	}
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn(
				"scheduled Action occurrence could not create Run",
				"target", definition.key.targetID,
				"action", definition.key.actionName,
				"trigger", definition.key.triggerName,
				"scheduled_at", scheduledAt,
				"diagnostic", creationDiagnostic(err),
			)
		}
		return
	}

	s.logger.Info(
		"scheduled Action occurrence created Run",
		"run", created.ID,
		"target", definition.key.targetID,
		"action", definition.key.actionName,
		"trigger", definition.key.triggerName,
		"scheduled_at", scheduledAt,
	)
}

func (s *Scheduler) listDefinitions(ctx context.Context) ([]scheduleDefinition, error) {
	definitions := []scheduleDefinition{}
	var offset int32
	for {
		page, err := s.catalog.ListTargets(ctx, catalog.TargetListFilter{
			Limit:  catalog.MaximumTargetListLimit,
			Offset: offset,
		})
		if err != nil {
			return nil, fmt.Errorf("list active Targets: %w", err)
		}
		for _, target := range page.Items {
			for _, actionName := range slices.Sorted(maps.Keys(target.Target.Spec.Actions)) {
				action := target.Target.Spec.Actions[actionName]
				for _, trigger := range action.Triggers {
					if trigger.Schedule == nil {
						continue
					}
					definitions = append(definitions, scheduleDefinition{
						key: scheduleKey{
							targetID:    target.ID,
							actionName:  actionName,
							triggerName: trigger.Name,
						},
						catalogRevisionID: target.CatalogRevisionID,
						cron:              trigger.Schedule.Cron,
						parameters:        maps.Clone(trigger.Parameters),
					})
				}
			}
		}

		offset += int32(len(page.Items))
		if len(page.Items) == 0 || int64(offset) >= page.Total {
			break
		}
	}

	return definitions, nil
}

func creationDiagnostic(err error) string {
	for _, known := range []error{
		run.ErrInvalidRequest,
		run.ErrInvalidParameters,
		run.ErrInvalidInputs,
		run.ErrRepositoryNotFound,
		run.ErrCatalogNotFound,
		run.ErrTargetNotFound,
		run.ErrActionNotFound,
		run.ErrRuntimeNotFound,
		run.ErrPlacementNotFound,
		run.ErrIdempotencyConflict,
		run.ErrScheduleNotFound,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}

	return "Run creation failed; check Server configuration and dependency health."
}
