// Package scheduletrigger evaluates repository-defined UTC cron schedules and
// creates ordinary immutable Runs.
package scheduletrigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/google/uuid"
	"k8s.io/utils/clock"

	"github.com/slipramp/slipramp/internal/cronexpr"
	"github.com/slipramp/slipramp/internal/run"
)

const DefaultEvaluationInterval = 5 * time.Second

type Catalog interface {
	ListScheduledTriggers(context.Context) ([]Definition, error)
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

// Definition identifies a scheduled Action trigger in an active catalog revision.
type Definition struct {
	TargetID          uuid.UUID
	ActionName        string
	TriggerName       string
	CatalogRevisionID uuid.UUID
	Cron              string
	Parameters        map[string]any
}

type scheduleState struct {
	revisionID uuid.UUID
	cron       string
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
	definitions, err := s.catalog.ListScheduledTriggers(ctx)
	if err != nil {
		return err
	}

	seen := make(map[scheduleKey]struct{}, len(definitions))
	for _, definition := range definitions {
		key := scheduleKey{definition.TargetID, definition.ActionName, definition.TriggerName}
		seen[key] = struct{}{}
		state, found := s.states[key]
		if !found || state.revisionID != definition.CatalogRevisionID || state.cron != definition.Cron {
			parsed, err := cronexpr.Parse(definition.Cron)
			if err != nil {
				return fmt.Errorf("parse active scheduled trigger: %w", err)
			}
			s.states[key] = scheduleState{
				revisionID: definition.CatalogRevisionID,
				cron:       definition.Cron,
				schedule:   parsed,
				next:       parsed.Next(now),
			}
			continue
		}

		if state.next.After(now) {
			continue
		}

		s.createOccurrence(ctx, definition, state.next)
		state.next = state.schedule.Next(now)
		s.states[key] = state
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
	definition Definition,
	scheduledAt time.Time,
) {
	idempotencyKey := run.ScheduleOccurrenceIdempotencyKey(
		definition.TargetID,
		definition.ActionName,
		definition.TriggerName,
		scheduledAt,
	)
	created, err := s.runs.CreateRun(ctx, run.CreateRequest{
		TargetID:   definition.TargetID,
		ActionName: definition.ActionName,
		Parameters: maps.Clone(definition.Parameters),
		Inputs:     map[string]uuid.UUID{},
		Source: run.Source{
			Type: run.SourceTypeSchedule,
			Schedule: &run.ScheduleRunSource{
				TriggerName: definition.TriggerName,
				ScheduledAt: scheduledAt,
			},
		},
		IdempotencyKey: &idempotencyKey,
	})
	if errors.Is(err, run.ErrScheduleOverlap) {
		s.logger.Info(
			"scheduled Action occurrence skipped because its previous Run is unfinished",
			"target", definition.TargetID,
			"action", definition.ActionName,
			"trigger", definition.TriggerName,
			"scheduled_at", scheduledAt,
		)
		return
	}
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn(
				"scheduled Action occurrence could not create Run",
				"target", definition.TargetID,
				"action", definition.ActionName,
				"trigger", definition.TriggerName,
				"scheduled_at", scheduledAt,
				"diagnostic", creationDiagnostic(err),
			)
		}
		return
	}

	s.logger.Info(
		"scheduled Action occurrence created Run",
		"run", created.ID,
		"target", definition.TargetID,
		"action", definition.ActionName,
		"trigger", definition.TriggerName,
		"scheduled_at", scheduledAt,
	)
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
