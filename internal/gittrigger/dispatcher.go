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
		now time.Time,
		diagnostic string,
	) (FailureState, bool, error)
}

type RunCreator interface {
	CreateRun(ctx context.Context, request run.CreateRequest) (run.Run, error)
}

type Dispatcher struct {
	deliveries Store
	runs       RunCreator
	interval   time.Duration
	logger     *slog.Logger
	now        func() time.Time
}

func NewDispatcher(deliveries Store, runs RunCreator, interval time.Duration, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		deliveries: deliveries,
		now:        time.Now,
		runs:       runs,
		interval:   interval,
		logger:     logger.With("component", "git_trigger_dispatcher"),
	}
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

	idempotencyKey := delivery.ID.String()
	revisionID := delivery.CatalogRevisionID
	created, err := d.runs.CreateRun(ctx, run.CreateRequest{
		RepositoryID:      delivery.RepositoryID,
		TargetName:        delivery.TargetName,
		ActionName:        delivery.ActionName,
		Parameters:        delivery.Parameters,
		Inputs:            map[string]uuid.UUID{},
		CatalogRevisionID: &revisionID,
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
		d.logger.Info(
			"Git-trigger delivery was superseded before Run creation",
			"delivery", delivery.ID,
			"repository", delivery.RepositoryID,
			"target", delivery.TargetName,
		)

		return true, nil
	}
	if err != nil {
		diagnostic := FailureDiagnostic(err)
		failure, recorded, recordErr := d.deliveries.RecordGitTriggerDeliveryFailure(
			ctx,
			delivery.ID,
			d.now(),
			diagnostic,
		)
		if recordErr != nil {
			return false, fmt.Errorf("record failed attempt for delivery %s: %w", delivery.ID, recordErr)
		}
		if !recorded {
			d.logger.Info("Git-trigger delivery was resolved after Run creation failed",
				"delivery", delivery.ID,
				"repository", delivery.RepositoryID,
				"target", delivery.TargetName,
			)
			return true, nil
		}
		d.logger.Warn("Git-trigger delivery could not create Run",
			"delivery", delivery.ID,
			"repository", delivery.RepositoryID,
			"target", delivery.TargetName,
			"action", delivery.ActionName,
			"trigger", delivery.TriggerName,
			"failure_count", failure.Count,
			"terminal", failure.Terminal,
			"diagnostic", diagnostic,
		)
		return true, nil
	}

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
