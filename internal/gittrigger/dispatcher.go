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
	GetPendingGitTriggerDelivery(ctx context.Context) (Delivery, error)
	CompleteGitTriggerDelivery(ctx context.Context, deliveryID uuid.UUID, runID uuid.UUID) error
}

type RunCreator interface {
	CreateRun(ctx context.Context, request run.CreateRequest) (run.Run, error)
}

type Dispatcher struct {
	deliveries Store
	runs       RunCreator
	interval   time.Duration
	logger     *slog.Logger
}

func NewDispatcher(deliveries Store, runs RunCreator, interval time.Duration, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		deliveries: deliveries,
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

// ProcessNext handles the oldest pending delivery, if one exists.
func (d *Dispatcher) ProcessNext(ctx context.Context) (bool, error) {
	delivery, err := d.deliveries.GetPendingGitTriggerDelivery(ctx)
	if errors.Is(err, ErrNoPendingDelivery) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get pending delivery: %w", err)
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
		return false, fmt.Errorf("create Run for delivery %s: %w", delivery.ID, err)
	}

	if err := d.deliveries.CompleteGitTriggerDelivery(ctx, delivery.ID, created.ID); err != nil {
		return false, fmt.Errorf("complete delivery %s: %w", delivery.ID, err)
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
