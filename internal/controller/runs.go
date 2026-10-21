package controller

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

func (c *Controller) handleAssignment(ctx context.Context, assignment api.RunAssignment) error {
	ensured, err := c.jobs.Ensure(ctx, assignment)
	if err != nil {
		c.logger.Warn(
			"materialize Run",
			"run_id", assignment.ID,
			"error", err,
		)
		if !isPermanentMaterializationError(err) {
			return nil
		}

		if sealErr := c.sealLog(ctx, assignment.ID, emptyLogSummary()); sealErr != nil {
			return sealErr
		}

		reason := "MaterializationFailed"
		message := err.Error()
		outcome := api.RunObservationOutcomeFailed
		reportErr := c.reports.ReportObservation(
			ctx,
			assignment.ID,
			api.RunObservation{
				Phase:   api.RunObservationPhaseFinished,
				Outcome: &outcome,
				Reason:  &reason,
				Message: &message,
			},
		)
		if errors.Is(reportErr, errControllerCredentialRejected) {
			return reportErr
		}
		if reportErr != nil {
			c.logger.Warn(
				"report materialization failure",
				"run_id", assignment.ID,
				"error", reportErr,
			)
		}

		return nil
	}

	c.logger.Info(
		"Run materialized",
		"run_id", assignment.ID,
		"job_created", ensured.created,
	)
	c.startRunObserver(ctx, ensured.handle, nil)

	return nil
}

func (c *Controller) handleCancellation(ctx context.Context, command api.CancelRunCommand) error {
	handle := jobHandle{
		runID:     command.ID,
		namespace: string(command.JobNamespace),
		name:      runJobName(command.ID),
	}
	if command.JobUID != nil {
		handle.uid = *command.JobUID
	}

	c.startRunObserver(ctx, handle, &command)

	return nil
}

func (c *Controller) startRunObserver(
	ctx context.Context,
	handle jobHandle,
	initialCancellation *api.CancelRunCommand,
) *runObserver {
	c.observerMutex.Lock()
	if observer := c.observedRuns[handle.runID]; observer != nil {
		c.observerMutex.Unlock()
		if initialCancellation != nil {
			select {
			case observer.cancellations <- *initialCancellation:
			case <-observer.done:
			case <-ctx.Done():
			}
		}

		return observer
	}

	observer := &runObserver{
		cancellations: make(chan api.CancelRunCommand, 1),
		done:          make(chan struct{}),
	}
	if initialCancellation != nil {
		observer.cancellations <- *initialCancellation
	}
	c.observedRuns[handle.runID] = observer
	c.observers.Add(1)
	c.observerMutex.Unlock()

	go c.observeRun(ctx, handle, observer)

	return observer
}

func (c *Controller) observeRun(ctx context.Context, handle jobHandle, observer *runObserver) {
	defer func() {
		c.observerMutex.Lock()
		if c.observedRuns[handle.runID] == observer {
			delete(c.observedRuns, handle.runID)
		}
		c.observerMutex.Unlock()
		close(observer.done)
		c.observers.Done()
	}()

	newRunObservationSession(c, handle, observer).run(ctx)
}

func (c *Controller) requestJobCancellation(
	ctx context.Context,
	command api.CancelRunCommand,
) (cancellationResult, error) {
	for {
		result, err := c.jobs.RequestCancellation(ctx, command)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return cancellationResult{}, ctx.Err()
		}

		c.logger.Warn(
			"request execution cancellation",
			"run_id", command.ID,
			"error", err,
		)
		if !waitForRetry(ctx, c.observeEvery) {
			return cancellationResult{}, ctx.Err()
		}
	}
}

func (c *Controller) finishUnstartedCancellation(ctx context.Context, handle jobHandle) {
	summary, err := c.emptyOrSealedLog(ctx, handle.runID)
	if err != nil {
		if isUnknownRunRejection(err) {
			c.logRunRejection(handle.runID, "finish unstarted cancellation", err)
		} else if ctx.Err() == nil {
			c.logger.Error(
				"finish unstarted cancellation",
				"run_id", handle.runID,
				"error", err,
			)
		}

		return
	}
	if err := c.sealLog(ctx, handle.runID, summary); err != nil {
		if isUnknownRunRejection(err) {
			c.logRunRejection(handle.runID, "seal canceled Run log", err)
		}

		return
	}

	outcome := api.RunObservationOutcomeCanceled
	reason := "CancelRequested"
	c.reportTerminalObservation(ctx, handle.runID, runObservation{
		phase:   api.RunObservationPhaseFinished,
		outcome: &outcome,
		jobUID:  optionalNonemptyString(handle.uid),
		reason:  &reason,
	})
}

func (c *Controller) finishObservedRun(
	ctx context.Context,
	handle jobHandle,
	observation runObservation,
	logStream *logSession,
	completedLog *logStreamResult,
) {
	var summary logSummary
	for {
		var err error
		summary, err = c.completeRunLog(ctx, handle, logStream, completedLog)
		if err == nil {
			break
		}
		if isUnknownRunRejection(err) {
			c.logRunRejection(handle.runID, "stream terminal Run log", err)

			return
		}
		if ctx.Err() != nil {
			return
		}

		c.logger.Warn(
			"finish terminal Run log",
			"run_id", handle.runID,
			"error", err,
		)
		logStream = nil
		completedLog = nil
		if !waitForRetry(ctx, c.observeEvery) {
			return
		}
	}
	if err := c.sealLog(ctx, handle.runID, summary); err != nil {
		if isUnknownRunRejection(err) {
			c.logRunRejection(handle.runID, "seal terminal Run log", err)
		}

		return
	}
	if !c.reportTerminalObservation(ctx, handle.runID, observation) {
		return
	}

	for {
		err := c.jobs.Delete(ctx, handle)
		if err == nil {
			c.logger.Info(
				"execution Job deleted",
				"run_id", handle.runID,
				"job_namespace", handle.namespace,
				"job_name", handle.name,
			)

			return
		}
		if ctx.Err() != nil {
			return
		}

		c.logger.Warn(
			"delete completed execution Job",
			"run_id", handle.runID,
			"job_namespace", handle.namespace,
			"job_name", handle.name,
			"error", err,
		)
		if !waitForRetry(ctx, c.observeEvery) {
			return
		}
	}
}

func (c *Controller) completeRunLog(
	ctx context.Context,
	handle jobHandle,
	logStream *logSession,
	completedLog *logStreamResult,
) (logSummary, error) {
	if completedLog != nil {
		return completedLog.summary, completedLog.err
	}
	if logStream == nil {
		var err error
		logStream, err = c.startLogSession(ctx, handle, false)
		if errors.Is(err, errLogNotReady) {
			return c.emptyOrSealedLog(ctx, handle.runID)
		}
		if err != nil {
			return logSummary{}, err
		}
	}

	return waitForLog(ctx, logStream)
}

func (c *Controller) emptyOrSealedLog(ctx context.Context, runID uuid.UUID) (logSummary, error) {
	cursor, err := c.getLogCursor(ctx, runID)
	if err != nil {
		return logSummary{}, err
	}
	if cursor.sealed {
		return logSummary{
			sizeBytes: cursor.nextOffsetBytes,
			sha256:    cursor.sha256,
			sealed:    true,
		}, nil
	}
	if cursor.nextOffsetBytes != 0 {
		return logSummary{}, errors.New("cannot finalize a nonempty Run log after its Kubernetes source disappeared")
	}

	return emptyLogSummary(), nil
}

func waitForLog(ctx context.Context, session *logSession) (logSummary, error) {
	select {
	case <-ctx.Done():
		return logSummary{}, ctx.Err()
	case result := <-session.result:
		return result.summary, result.err
	}
}

func (c *Controller) reportTerminalObservation(ctx context.Context, runID uuid.UUID, observation runObservation) bool {
	for {
		err := c.reports.ReportObservation(ctx, runID, observationRequest(observation))
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		if errors.Is(err, errRunObservationRejected) {
			c.logRunRejection(runID, "report terminal observation", err)

			return false
		}

		c.logger.Warn(
			"report terminal Run observation",
			"run_id", runID,
			"error", err,
		)
		if !waitForRetry(ctx, c.observeEvery) {
			return false
		}
	}
}

func (c *Controller) logRunRejection(runID uuid.UUID, operation string, err error) {
	c.logger.Warn(
		"stop observing Run rejected by Server",
		"run_id", runID,
		"operation", operation,
		"error", err,
	)
}

func isUnknownRunRejection(err error) bool {
	if !errors.Is(err, errRunLogRejected) &&
		!errors.Is(err, errRunObservationRejected) {
		return false
	}

	statusCode, ok := serverResponseStatusCode(err)

	return ok && statusCode == 404
}

func observationRequest(observation runObservation) api.RunObservation {
	return api.RunObservation{
		Phase:    observation.phase,
		Outcome:  observation.outcome,
		JobUID:   observation.jobUID,
		Reason:   observation.reason,
		Message:  observation.message,
		ExitCode: observation.exitCode,
	}
}
