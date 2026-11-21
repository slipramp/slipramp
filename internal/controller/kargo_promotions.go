package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	controllerkargo "github.com/slipramp/slipramp/internal/controller/kargo"
)

const kargoPromotionOperationTimeout = 15 * time.Second

type kargoPromotionCreator interface {
	Ensure(context.Context, api.KargoPromotionCreationCommand) (controllerkargo.PromotionMaterialization, error)
}

type kargoPromotionCanceler interface {
	Cancel(context.Context, api.KargoPromotionReconciliationCommand) (*controllerkargo.PromotionObservation, error)
}

type kargoPromotionReconciler interface {
	Reconcile(
		context.Context,
		api.KargoPromotionReconciliationCommand,
	) (*controllerkargo.PromotionObservation, error)
}

func (c *Controller) handleKargoPromotionCreation(
	ctx context.Context,
	command api.KargoPromotionCreationCommand,
) error {
	var materialization controllerkargo.PromotionMaterialization
	var materializeErr error
	permanentFailure := c.promotions == nil
	if permanentFailure {
		//nolint:staticcheck // This operator-facing diagnostic begins with the Kargo product name.
		materializeErr = fmt.Errorf(
			"Kargo integration %q is not configured on this Controller",
			command.Integration,
		)
	} else {
		attemptContext, cancel := context.WithTimeout(ctx, kargoPromotionOperationTimeout)
		materialization, materializeErr = c.promotions.Ensure(attemptContext, command)
		cancel()
		permanentFailure = controllerkargo.IsPermanentPromotionError(materializeErr)
	}

	report := api.KargoPromotionDispatchReport{Attempt: command.Attempt}
	if materializeErr == nil {
		report.Result = api.KargoPromotionDispatchReportResultCreated
		report.KargoName = &materialization.Name
		report.KargoUID = &materialization.UID
	} else {
		diagnostic := controllerkargo.BoundedPromotionDiagnostic(materializeErr.Error())
		report.Diagnostic = &diagnostic
		if permanentFailure {
			report.Result = api.KargoPromotionDispatchReportResultFailed
		} else {
			report.Result = api.KargoPromotionDispatchReportResultRetryable
		}
	}

	reportErr := c.promotionReports.ReportKargoPromotionDispatch(ctx, command.ID, report)
	if permanentServerError(reportErr) {
		return fmt.Errorf("report Kargo Promotion %s creation: %w", command.ID, reportErr)
	}
	if reportErr != nil {
		c.logger.Warn(
			"report Kargo Promotion creation",
			"promotion_id", command.ID,
			"attempt", command.Attempt,
			"error", reportErr,
		)

		return nil
	}
	if materializeErr != nil {
		c.logger.Warn(
			"materialize Kargo Promotion",
			"promotion_id", command.ID,
			"attempt", command.Attempt,
			"terminal", permanentFailure,
			"error", materializeErr,
		)

		return nil
	}

	c.logger.Info(
		"Kargo Promotion materialized",
		"promotion_id", command.ID,
		"kargo_name", materialization.Name,
		"kargo_uid", materialization.UID,
		"created", materialization.Created,
	)

	return nil
}

func (c *Controller) handleKargoPromotionReconciliation(
	ctx context.Context,
	command api.KargoPromotionReconciliationCommand,
) error {
	if c.promotionReconciler == nil {
		c.logger.Warn("Kargo reconciliation integration unavailable", "integration", command.Integration)
		return nil
	}
	return c.observeKargoPromotion(ctx, command, c.promotionReconciler.Reconcile)
}

func (c *Controller) handleKargoPromotionCancellation(ctx context.Context, command api.KargoPromotionReconciliationCommand) error {
	if c.promotionCanceler == nil {
		c.logger.Warn("Kargo cancellation integration unavailable", "integration", command.Integration)
		return nil
	}
	return c.observeKargoPromotion(ctx, command, c.promotionCanceler.Cancel)
}

func (c *Controller) observeKargoPromotion(
	ctx context.Context,
	command api.KargoPromotionReconciliationCommand,
	observe func(context.Context, api.KargoPromotionReconciliationCommand) (*controllerkargo.PromotionObservation, error),
) error {
	attemptContext, cancel := context.WithTimeout(ctx, kargoPromotionOperationTimeout)
	observation, err := observe(attemptContext, command)
	cancel()
	if err != nil {
		c.logger.Warn(
			"reconcile Kargo Promotion",
			"promotion_id", command.ID,
			"kargo_name", command.KargoName,
			"kargo_uid", command.KargoUID,
			"error", err,
		)

		return nil
	}
	if recovery := command.Recovery; recovery != nil {
		report := api.KargoPromotionDispatchReport{
			Attempt: recovery.Attempt,
			Result:  api.KargoPromotionDispatchReportResultAbsent,
		}
		if observation != nil {
			report.Result = api.KargoPromotionDispatchReportResultCreated
			report.KargoUID = &observation.UID
			report.KargoName = &observation.Name
		}
		if err := c.promotionReports.ReportKargoPromotionDispatch(ctx, command.ID, report); err != nil {
			if permanentServerError(err) {
				return fmt.Errorf("report Kargo Promotion %s recovery: %w", command.ID, err)
			}
			c.logger.Warn("report Kargo Promotion recovery", "promotion_id", command.ID, "error", err)
		}
		return nil
	}
	if observation == nil {
		return nil
	}
	if command.KargoUID != nil && observation.Phase == nil {
		return nil
	}

	var acknowledged *bool
	if observation.CancellationAcknowledged {
		acknowledged = new(true)
	}
	reportErr := c.promotionReconciliationReports.ReportKargoPromotionReconciliation(
		ctx,
		command.ID,
		api.KargoPromotionReconciliationReport{
			KargoName:                observation.Name,
			KargoUID:                 observation.UID,
			Phase:                    observation.Phase,
			CancellationAcknowledged: acknowledged,
			Diagnostic:               observation.Diagnostic,
		},
	)
	if permanentServerError(reportErr) {
		return fmt.Errorf("report Kargo Promotion %s reconciliation: %w", command.ID, reportErr)
	}
	if reportErr != nil {
		c.logger.Warn(
			"report Kargo Promotion reconciliation",
			"promotion_id", command.ID,
			"phase", observation.Phase,
			"error", reportErr,
		)

		return nil
	}

	c.logger.Info(
		"Kargo Promotion observation reconciled",
		"promotion_id", command.ID,
		"kargo_name", observation.Name,
		"kargo_uid", observation.UID,
		"phase", observation.Phase,
	)

	return nil
}
