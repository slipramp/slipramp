package gittrigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/run"
)

const DefaultDispatchInterval = time.Second

type Store interface {
	GetEligibleGitTriggerDelivery(ctx context.Context, now time.Time) (Delivery, error)
	RecordGitTriggerDeliveryFailure(
		ctx context.Context,
		deliveryID uuid.UUID,
		expectedFailureCount int,
		now time.Time,
		failure CreationFailure,
	) (Delivery, bool, error)
}

type RunCreator interface {
	CreateRun(ctx context.Context, request run.CreateRequest) (run.Run, error)
}

type DeliveryAttemptResult string

const (
	DeliveryAttemptPendingEligible DeliveryAttemptResult = "pending-eligible"
	DeliveryAttemptRunLinked       DeliveryAttemptResult = "run-linked"
	DeliveryAttemptSuperseded      DeliveryAttemptResult = "superseded"
	DeliveryAttemptPendingRetry    DeliveryAttemptResult = "pending-retry"
	DeliveryAttemptFailed          DeliveryAttemptResult = "failed"
	DeliveryAttemptRecordFailed    DeliveryAttemptResult = "record_failed"
)

type DeliveryObserver interface {
	ObserveGitTriggerDelivery(context.Context, DeliveryAttemptResult, time.Duration)
}

type DispatcherOption func(*Dispatcher)

func WithDeliveryObserver(observer DeliveryObserver) DispatcherOption {
	return func(dispatcher *Dispatcher) {
		if observer != nil {
			dispatcher.observer = observer
		}
	}
}

type noopDeliveryObserver struct{}

func (noopDeliveryObserver) ObserveGitTriggerDelivery(context.Context, DeliveryAttemptResult, time.Duration) {
}

type Dispatcher struct {
	deliveries Store
	runs       RunCreator
	interval   time.Duration
	logger     *slog.Logger
	now        func() time.Time
	observer   DeliveryObserver
}

func NewDispatcher(
	deliveries Store,
	runs RunCreator,
	interval time.Duration,
	logger *slog.Logger,
	options ...DispatcherOption,
) *Dispatcher {
	dispatcher := &Dispatcher{
		deliveries: deliveries,
		now:        time.Now,
		runs:       runs,
		interval:   interval,
		logger:     logger.With("component", "git_trigger_dispatcher"),
		observer:   noopDeliveryObserver{},
	}
	for _, option := range options {
		option(dispatcher)
	}

	return dispatcher
}

func (d *Dispatcher) Run(ctx context.Context) {
	d.drain(ctx)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.drain(ctx)
		}
	}
}

func (d *Dispatcher) drain(ctx context.Context) {
	for ctx.Err() == nil {
		processed, err := d.ProcessNext(ctx)
		if err != nil {
			if ctx.Err() == nil {
				d.logger.Warn("dispatch Git-trigger delivery", "error", err)
			}

			return
		}
		if !processed {
			return
		}
	}
}

// ProcessNext handles the oldest eligible delivery, if one exists.
func (d *Dispatcher) ProcessNext(ctx context.Context) (bool, error) {
	delivery, err := d.deliveries.GetEligibleGitTriggerDelivery(ctx, d.now())
	if errors.Is(err, ErrNoEligibleDelivery) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get eligible delivery: %w", err)
	}
	startedAt := time.Now()

	idempotencyKey := delivery.ID.String()
	revisionID := delivery.CatalogRevisionID
	created, err := d.runs.CreateRun(ctx, run.CreateRequest{
		TargetID:          delivery.TargetID,
		ActionName:        delivery.ActionName,
		Parameters:        delivery.Parameters,
		Inputs:            map[string]uuid.UUID{},
		CatalogRevisionID: &revisionID,
		ReviewRequired:    delivery.ReviewRequired,
		Source: run.Source{
			Type: run.SourceTypeGit,
			Git: &run.GitRunSource{
				TriggerName: delivery.TriggerName,
				DeliveryID:  delivery.ID,
			},
		},
		IdempotencyKey: &idempotencyKey,
	})
	if errors.Is(err, run.ErrGitRunSuperseded) {
		d.observer.ObserveGitTriggerDelivery(ctx, DeliveryAttemptSuperseded, time.Since(startedAt))
		d.logger.Info(
			"Git-trigger delivery was superseded before Run creation",
			"delivery", delivery.ID,
			"repository", delivery.RepositoryID,
			"target", delivery.TargetName,
		)

		return true, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return true, nil
		}

		failure := ClassifyCreationFailure(err)
		updated, recorded, recordErr := d.deliveries.RecordGitTriggerDeliveryFailure(
			ctx,
			delivery.ID,
			delivery.Lifecycle.FailureCount,
			d.now(),
			failure,
		)
		if recordErr != nil {
			d.observer.ObserveGitTriggerDelivery(ctx, DeliveryAttemptRecordFailed, time.Since(startedAt))
			return false, fmt.Errorf("record failed attempt for delivery %s: %w", delivery.ID, recordErr)
		}
		if !recorded {
			state, stateErr := updated.State(d.now())
			if stateErr != nil {
				d.observer.ObserveGitTriggerDelivery(ctx, DeliveryAttemptRecordFailed, time.Since(startedAt))
				return false, fmt.Errorf("interpret concurrently resolved delivery %s: %w", delivery.ID, stateErr)
			}
			d.observer.ObserveGitTriggerDelivery(ctx, DeliveryAttemptResult(state), time.Since(startedAt))
			d.logger.Info("Git-trigger delivery was resolved after Run creation failed",
				"delivery", delivery.ID,
				"repository", delivery.RepositoryID,
				"target", delivery.TargetName,
				"state", state,
			)
			return true, nil
		}
		state, stateErr := updated.State(d.now())
		if stateErr != nil {
			d.observer.ObserveGitTriggerDelivery(ctx, DeliveryAttemptRecordFailed, time.Since(startedAt))
			return false, fmt.Errorf("interpret failed delivery %s: %w", delivery.ID, stateErr)
		}
		result := DeliveryAttemptPendingRetry
		if state == StateFailed {
			result = DeliveryAttemptFailed
		}
		d.observer.ObserveGitTriggerDelivery(ctx, result, time.Since(startedAt))
		d.logger.Warn("Git-trigger delivery could not create Run",
			"delivery", delivery.ID,
			"repository", delivery.RepositoryID,
			"target", delivery.TargetName,
			"action", delivery.ActionName,
			"trigger", delivery.TriggerName,
			"failure_count", updated.Lifecycle.FailureCount,
			"state", state,
			"diagnostic", failure.Diagnostic,
		)
		return true, nil
	}

	d.observer.ObserveGitTriggerDelivery(ctx, DeliveryAttemptRunLinked, time.Since(startedAt))
	d.logger.Info(
		"Git-trigger delivery created Run",
		"delivery", delivery.ID,
		"run", created.ID,
		"repository", delivery.RepositoryID,
		"target", delivery.TargetName,
		"action", delivery.ActionName,
		"trigger", delivery.TriggerName,
	)

	return true, nil
}
