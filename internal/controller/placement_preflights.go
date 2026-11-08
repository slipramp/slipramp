package controller

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controller/preflight"
)

const preflightCleanupTimeout = 30 * time.Second

type placementPreflightJobs interface {
	Ensure(context.Context, api.PlacementPreflightCommand) (preflight.Handle, error)
	Observe(context.Context, preflight.Handle) (preflight.Observation, error)
	Delete(context.Context, preflight.Handle) error
}

func (c *Controller) handlePlacementPreflight(ctx context.Context, command api.PlacementPreflightCommand) {
	c.preflightMutex.Lock()
	if _, active := c.activePreflights[command.ID]; active {
		c.preflightMutex.Unlock()
		return
	}
	c.activePreflights[command.ID] = struct{}{}
	c.preflightMutex.Unlock()

	c.workers.Go(func() {
		defer func() {
			c.preflightMutex.Lock()
			delete(c.activePreflights, command.ID)
			c.preflightMutex.Unlock()
		}()
		c.executePlacementPreflight(ctx, command)
	})
}

func (c *Controller) executePlacementPreflight(ctx context.Context, command api.PlacementPreflightCommand) {
	if c.preflightSlots != nil {
		select {
		case c.preflightSlots <- struct{}{}:
			defer func() { <-c.preflightSlots }()
		case <-ctx.Done():
			return
		}
	}

	handle, err := c.preflightJobs.Ensure(ctx, command)
	if err != nil {
		reason := "MaterializationFailed"
		message := preflight.BoundMessage(err.Error(), "The preflight Job could not be created.")
		c.reportPlacementPreflightUntilAccepted(ctx, command.ID, api.PlacementPreflightReport{
			State:        api.PlacementPreflightReportStateFailed,
			CleanupState: api.PlacementPreflightReportCleanupStateNotRequired,
			Reason:       &reason,
			Message:      &message,
		})
		return
	}

	jobUID := handle.UID
	if !c.reportPlacementPreflightUntilAccepted(ctx, command.ID, api.PlacementPreflightReport{
		State:        api.PlacementPreflightReportStateRunning,
		CleanupState: api.PlacementPreflightReportCleanupStatePending,
		JobUID:       &jobUID,
	}) {
		cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), preflightCleanupTimeout)
		_ = c.preflightJobs.Delete(cleanupContext, handle)
		cancelCleanup()
		return
	}

	var terminal preflight.Observation
	for {
		observation, err := c.preflightJobs.Observe(ctx, handle)
		if err == nil && observation.Finished {
			terminal = observation
			break
		}
		if err != nil {
			c.logger.Warn("observe Placement preflight", "preflight_id", command.ID, "error", err)
		}
		if !waitForRetry(ctx, c.observeEvery) {
			return
		}
	}

	cleanupState := api.PlacementPreflightReportCleanupStateSucceeded
	cleanupContext, cancelCleanup := context.WithTimeout(ctx, preflightCleanupTimeout)
	cleanupErr := c.preflightJobs.Delete(cleanupContext, handle)
	cancelCleanup()
	if cleanupErr != nil {
		cleanupState = api.PlacementPreflightReportCleanupStateFailed
		terminal.Succeeded = false
		terminal.Reason = "CleanupFailed"
		terminal.Message = preflight.BoundMessage(cleanupErr.Error(), "The probe Job could not be cleaned up.")
	}

	state := api.PlacementPreflightReportStateFailed
	if terminal.Succeeded {
		state = api.PlacementPreflightReportStateSucceeded
	}
	diagnostics := terminal.Diagnostics
	reason := preflight.BoundReason(terminal.Reason, "ProbeFailed")
	message := preflight.BoundMessage(terminal.Message, "The Placement preflight failed.")
	c.reportPlacementPreflightUntilAccepted(ctx, command.ID, api.PlacementPreflightReport{
		State:        state,
		CleanupState: cleanupState,
		JobUID:       &jobUID,
		Reason:       &reason,
		Message:      &message,
		Diagnostics:  &diagnostics,
	})
}

func (c *Controller) reportPlacementPreflightUntilAccepted(
	ctx context.Context,
	preflightID uuid.UUID,
	report api.PlacementPreflightReport,
) bool {
	for {
		if err := c.preflightReports.ReportPlacementPreflight(ctx, preflightID, report); err == nil {
			return true
		} else if permanentServerError(err) {
			if errors.Is(err, errControllerCredentialRejected) && c.stop != nil {
				c.stop(err)
			}
			c.logger.Warn("stop reporting Placement preflight", "preflight_id", preflightID, "error", err)
			return false
		} else if ctx.Err() == nil {
			c.logger.Warn("report Placement preflight", "preflight_id", preflightID, "error", err)
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return false
		}
	}
}
