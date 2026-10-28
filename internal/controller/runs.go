package controller

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

func (c *Controller) handleAssignment(ctx context.Context, assignment api.RunAssignment) error {
	handle := jobHandle{
		runID:     assignment.ID,
		namespace: string(assignment.Placement.JobNamespace),
		name:      runJobName(assignment.ID),
	}
	c.startRunWorker(ctx, handle, &assignment, nil)

	return nil
}

func (c *Controller) materializeRun(ctx context.Context, assignment api.RunAssignment) (jobHandle, bool) {
	ensured, err := c.jobs.Ensure(ctx, assignment)
	if err != nil {
		if errors.Is(err, errRunJobIdentityMismatch) {
			c.runWorkerError(assignment.ID, "stop materialization after Job identity mismatch", err)

			return jobHandle{}, false
		}
		c.logger.Warn(
			"materialize Run",
			"run_id", assignment.ID,
			"error", err,
		)
		if !isPermanentMaterializationError(err) {
			return jobHandle{}, false
		}

		if sealErr := c.sealLog(ctx, assignment.ID, emptyLogSummary()); sealErr != nil {
			c.runWorkerError(assignment.ID, "seal materialization failure log", sealErr)

			return jobHandle{}, false
		}

		reason := "MaterializationFailed"
		message := err.Error()
		outcome := api.RunObservationOutcomeFailed
		c.reportTerminalObservation(ctx, assignment.ID, runObservation{
			phase:   api.RunObservationPhaseFinished,
			outcome: &outcome,
			reason:  &reason,
			message: &message,
		})

		return jobHandle{}, false
	}

	c.logger.Info(
		"Run materialized",
		"run_id", assignment.ID,
		"job_created", ensured.created,
	)
	return ensured.handle, true
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

	c.startRunWorker(ctx, handle, nil, &command)

	return nil
}

func (c *Controller) startRunWorker(
	ctx context.Context,
	handle jobHandle,
	assignment *api.RunAssignment,
	initialCancellation *api.CancelRunCommand,
) *runWorker {
	c.runMutex.Lock()
	if observer := c.activeRuns[handle.runID]; observer != nil {
		c.runMutex.Unlock()
		if initialCancellation != nil {
			select {
			case observer.cancellations <- *initialCancellation:
			default:
			}
		}

		return observer
	}

	observer := &runWorker{
		cancellations: make(chan api.CancelRunCommand, 1),
		done:          make(chan struct{}),
	}
	if initialCancellation != nil {
		observer.cancellations <- *initialCancellation
	}
	c.activeRuns[handle.runID] = observer
	c.runWorkers.Add(1)
	c.runMutex.Unlock()

	go c.executeRun(ctx, handle, assignment, observer)

	return observer
}

func (c *Controller) executeRun(
	ctx context.Context,
	handle jobHandle,
	assignment *api.RunAssignment,
	observer *runWorker,
) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		close(observer.done)
		c.runWorkers.Done()
	}()

	if assignment != nil {
		if c.runSlots != nil {
			select {
			case c.runSlots <- struct{}{}:
				defer func() { <-c.runSlots }()
			case command := <-observer.cancellations:
				session := newRunObservationSession(c, handle, observer)
				session.cancellation = &command
				session.run(ctx)

				return
			case <-ctx.Done():
				return
			}
		}

		select {
		case command := <-observer.cancellations:
			session := newRunObservationSession(c, handle, observer)
			session.cancellation = &command
			session.run(ctx)

			return
		default:
		}

		var materialized bool
		handle, materialized = c.materializeRun(ctx, *assignment)
		if !materialized {
			return
		}
	}

	newRunObservationSession(c, handle, observer).run(ctx)
}

func (c *Controller) reapRunWorkers() {
	c.runMutex.Lock()
	defer c.runMutex.Unlock()

	// Keep completed workers through any in-flight poll response, so a stale
	// assignment cannot recreate a Job after terminal reporting and deletion.
	for runID, worker := range c.activeRuns {
		select {
		case <-worker.done:
			if !worker.blocked {
				delete(c.activeRuns, runID)
			}
		default:
		}
	}
}

func (c *Controller) runWorkerError(runID uuid.UUID, operation string, err error) {
	if permanentRunError(err) {
		c.runMutex.Lock()
		if worker := c.activeRuns[runID]; worker != nil {
			worker.blocked = true
		}
		c.runMutex.Unlock()
	}
	if errors.Is(err, errControllerCredentialRejected) && c.stop != nil {
		c.stop(err)
	}
	c.logger.Warn(operation, "run_id", runID, "error", err)
}

func permanentRunError(err error) bool {
	return permanentServerError(err) || errors.Is(err, errRunJobIdentityMismatch)
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
		if errors.Is(err, errRunJobIdentityMismatch) {
			c.runWorkerError(command.ID, "stop cancellation after Job identity mismatch", err)

			return cancellationResult{}, err
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
	summary, err := c.closeMissingRunLog(ctx, handle.runID)
	if err != nil {
		if ctx.Err() == nil {
			c.runWorkerError(handle.runID, "finish unstarted cancellation", err)
		}

		return
	}
	if err := c.sealLog(ctx, handle.runID, summary); err != nil {
		if ctx.Err() == nil {
			c.runWorkerError(handle.runID, "seal canceled Run log", err)
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
		if permanentRunError(err) {
			c.runWorkerError(handle.runID, "stream terminal Run log", err)

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
		if ctx.Err() == nil {
			c.runWorkerError(handle.runID, "seal terminal Run log", err)
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
		if errors.Is(err, errRunJobIdentityMismatch) {
			c.runWorkerError(handle.runID, "retain completed Job after identity mismatch", err)

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
			return c.closeMissingRunLog(ctx, handle.runID)
		}
		if err != nil {
			return logSummary{}, err
		}
	}

	return waitForLog(ctx, logStream)
}

func (c *Controller) closeMissingRunLog(ctx context.Context, runID uuid.UUID) (logSummary, error) {
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
	if cursor.failed {
		return logSummary{sizeBytes: cursor.nextOffsetBytes, failed: true}, nil
	}
	if cursor.nextOffsetBytes != 0 {
		if err := c.failLog(ctx, runID); err != nil {
			return logSummary{}, err
		}
		c.logger.Warn(
			"Run log source disappeared; retained durable prefix as failed",
			"run_id", runID,
			"size_bytes", cursor.nextOffsetBytes,
		)

		return logSummary{sizeBytes: cursor.nextOffsetBytes, failed: true}, nil
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
		if permanentServerError(err) {
			c.runWorkerError(runID, "report terminal observation", err)

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

func observationRequest(observation runObservation) api.RunObservation {
	request := api.RunObservation{
		Phase:    observation.phase,
		Outcome:  observation.outcome,
		JobUID:   observation.jobUID,
		Reason:   observation.reason,
		Message:  observation.message,
		ExitCode: observation.exitCode,
	}
	if observation.images.Action != nil || observation.images.Checkout != nil || observation.images.Harness != nil {
		request.Images = &observation.images
	}

	return request
}
