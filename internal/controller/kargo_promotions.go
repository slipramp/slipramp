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
		c.logger.Warn(
			"reconcile Kargo Promotion without a configured integration",
			"promotion_id", command.ID,
			"integration", command.Integration,
		)

		return nil
	}

	attemptContext, cancel := context.WithTimeout(ctx, kargoPromotionOperationTimeout)
	observation, err := c.promotionReconciler.Reconcile(attemptContext, command)
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
	if observation == nil {
		return nil
	}
	if command.KargoUID != nil && observation.Phase == nil {
		return nil
	}

	reportErr := c.promotionReconciliationReports.ReportKargoPromotionReconciliation(
		ctx,
		command.ID,
		api.KargoPromotionReconciliationReport{
			KargoName:  observation.Name,
			KargoUID:   observation.UID,
			Phase:      observation.Phase,
			Diagnostic: observation.Diagnostic,
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
