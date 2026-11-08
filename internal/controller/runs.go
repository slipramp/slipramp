package controller

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controller/runjob"
)

type runJobs interface {
	Ensure(context.Context, api.RunAssignment) (runjob.Ensured, error)
	RequestCancellation(context.Context, api.CancelRunCommand) (runjob.CancellationResult, error)
	Recover(context.Context) ([]runjob.Handle, error)
	Observe(context.Context, runjob.Handle) (runjob.Observation, error)
	OpenLog(context.Context, runjob.Handle, bool) (io.ReadCloser, error)
	Delete(context.Context, runjob.Handle) error
}

func (c *Controller) handleAssignment(ctx context.Context, assignment api.RunAssignment) error {
	handle := runjob.Handle{
		RunID:     assignment.ID,
		Namespace: string(assignment.Placement.JobNamespace),
	}
	c.startRunWorker(ctx, handle, &assignment, nil)

	return nil
}

func (c *Controller) materializeRun(ctx context.Context, assignment api.RunAssignment) (runjob.Handle, bool) {
	ensured, err := c.jobs.Ensure(ctx, assignment)
	if err != nil {
		if errors.Is(err, runjob.ErrRunJobIdentityMismatch) {
			c.runWorkerError(assignment.ID, "stop materialization after Job identity mismatch", err)

			return runjob.Handle{}, false
		}
		c.logger.Warn(
			"materialize Run",
			"run_id", assignment.ID,
			"error", err,
		)
		if !runjob.IsPermanentMaterializationError(err) {
			return runjob.Handle{}, false
		}

		if sealErr := c.sealLog(ctx, assignment.ID, emptyLogSummary()); sealErr != nil {
			c.runWorkerError(assignment.ID, "seal materialization failure log", sealErr)

			return runjob.Handle{}, false
		}

		reason := "MaterializationFailed"
		message := err.Error()
		outcome := api.RunObservationOutcomeFailed
		c.reportTerminalObservation(ctx, assignment.ID, runjob.Observation{
			Phase:   api.RunObservationPhaseFinished,
			Outcome: &outcome,
			Reason:  &reason,
			Message: &message,
		})

		return runjob.Handle{}, false
	}

	c.logger.Info(
		"Run materialized",
		"run_id", assignment.ID,
		"job_created", ensured.Created,
	)
	return ensured.Handle, true
}

func (c *Controller) handleCancellation(ctx context.Context, command api.CancelRunCommand) error {
	handle := runjob.Handle{
		RunID:     command.ID,
		Namespace: string(command.JobNamespace),
	}
	if command.JobUID != nil {
		handle.UID = *command.JobUID
	}

	c.startRunWorker(ctx, handle, nil, &command)

	return nil
}

func (c *Controller) startRunWorker(
	ctx context.Context,
	handle runjob.Handle,
	assignment *api.RunAssignment,
	initialCancellation *api.CancelRunCommand,
) *runWorker {
	c.runMutex.Lock()
	if observer := c.activeRuns[handle.RunID]; observer != nil {
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
	c.activeRuns[handle.RunID] = observer
	c.runWorkers.Add(1)
	c.runMutex.Unlock()

	go c.executeRun(ctx, handle, assignment, observer)

	return observer
}

func (c *Controller) executeRun(
	ctx context.Context,
	handle runjob.Handle,
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
	return permanentServerError(err) || errors.Is(err, runjob.ErrRunJobIdentityMismatch)
}

func (c *Controller) requestJobCancellation(
	ctx context.Context,
	command api.CancelRunCommand,
) (runjob.CancellationResult, error) {
	for {
		result, err := c.jobs.RequestCancellation(ctx, command)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, runjob.ErrRunJobIdentityMismatch) {
			c.runWorkerError(command.ID, "stop cancellation after Job identity mismatch", err)

			return runjob.CancellationResult{}, err
		}
		if ctx.Err() != nil {
			return runjob.CancellationResult{}, ctx.Err()
		}

		c.logger.Warn(
			"request execution cancellation",
			"run_id", command.ID,
			"error", err,
		)
		if !waitForRetry(ctx, c.observeEvery) {
			return runjob.CancellationResult{}, ctx.Err()
		}
	}
}

func (c *Controller) finishUnstartedCancellation(ctx context.Context, handle runjob.Handle) {
	summary, err := c.closeMissingRunLog(ctx, handle.RunID)
	if err != nil {
		if ctx.Err() == nil {
			c.runWorkerError(handle.RunID, "finish unstarted cancellation", err)
		}

		return
	}
	if err := c.sealLog(ctx, handle.RunID, summary); err != nil {
		if ctx.Err() == nil {
			c.runWorkerError(handle.RunID, "seal canceled Run log", err)
		}

		return
	}

	outcome := api.RunObservationOutcomeCanceled
	reason := "CancelRequested"
	var jobUID *string
	if handle.UID != "" {
		jobUID = new(handle.UID)
	}
	c.reportTerminalObservation(ctx, handle.RunID, runjob.Observation{
		Phase:   api.RunObservationPhaseFinished,
		Outcome: &outcome,
		JobUID:  jobUID,
		Reason:  &reason,
	})
}

func (c *Controller) finishObservedRun(
	ctx context.Context,
	handle runjob.Handle,
	observation runjob.Observation,
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
			c.runWorkerError(handle.RunID, "stream terminal Run log", err)

			return
		}
		if ctx.Err() != nil {
			return
		}

		c.logger.Warn(
			"finish terminal Run log",
			"run_id", handle.RunID,
			"error", err,
		)
		logStream = nil
		completedLog = nil
		if !waitForRetry(ctx, c.observeEvery) {
			return
		}
	}
	if err := c.sealLog(ctx, handle.RunID, summary); err != nil {
		if ctx.Err() == nil {
			c.runWorkerError(handle.RunID, "seal terminal Run log", err)
		}

		return
	}
	if !c.reportTerminalObservation(ctx, handle.RunID, observation) {
		return
	}

	for {
		err := c.jobs.Delete(ctx, handle)
		if err == nil {
			c.logger.Info(
				"execution Job deleted",
				"run_id", handle.RunID,
				"job_namespace", handle.Namespace,
				"job_name", handle.Name,
			)

			return
		}
		if errors.Is(err, runjob.ErrRunJobIdentityMismatch) {
			c.runWorkerError(handle.RunID, "retain completed Job after identity mismatch", err)

			return
		}
		if ctx.Err() != nil {
			return
		}

		c.logger.Warn(
			"delete completed execution Job",
			"run_id", handle.RunID,
			"job_namespace", handle.Namespace,
			"job_name", handle.Name,
			"error", err,
		)
		if !waitForRetry(ctx, c.observeEvery) {
			return
		}
	}
}

func (c *Controller) completeRunLog(
	ctx context.Context,
	handle runjob.Handle,
	logStream *logSession,
	completedLog *logStreamResult,
) (logSummary, error) {
	if completedLog != nil {
		return completedLog.summary, completedLog.err
	}
	if logStream == nil {
		var err error
		logStream, err = c.startLogSession(ctx, handle, false)
		if errors.Is(err, runjob.ErrLogNotReady) {
			return c.closeMissingRunLog(ctx, handle.RunID)
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

func (c *Controller) reportTerminalObservation(ctx context.Context, runID uuid.UUID, observation runjob.Observation) bool {
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

func observationRequest(observation runjob.Observation) api.RunObservation {
	request := api.RunObservation{
		Phase:    observation.Phase,
		Outcome:  observation.Outcome,
		JobUID:   observation.JobUID,
		Reason:   observation.Reason,
		Message:  observation.Message,
		ExitCode: observation.ExitCode,
	}
	if observation.Images.Action != nil || observation.Images.Checkout != nil || observation.Images.Harness != nil {
		request.Images = &observation.Images
	}

	return request
}
