package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/controller/runjob"
)

type runObservationSession struct {
	controller   *Controller
	handle       runjob.Handle
	observer     *runWorker
	cancellation *api.CancelRunCommand
	logStream    *logSession
	completedLog *logStreamResult
	reported     *runjob.Observation
	reportedAt   time.Time
}

func newRunObservationSession(
	controller *Controller,
	handle runjob.Handle,
	observer *runWorker,
) *runObservationSession {
	return &runObservationSession{
		controller: controller,
		handle:     handle,
		observer:   observer,
	}
}

func (s *runObservationSession) run(ctx context.Context) {
	for {
		if s.completedLog != nil && permanentServerError(s.completedLog.err) {
			s.controller.runWorkerError(s.handle.RunID, "stream Run log", s.completedLog.err)

			return
		}
		s.acceptPendingCancellation()
		if s.cancellation != nil {
			if s.cancel(ctx) {
				return
			}

			continue
		}

		if s.observe(ctx) || s.wait(ctx) {
			return
		}
	}
}

func (s *runObservationSession) acceptPendingCancellation() {
	if s.cancellation != nil {
		return
	}

	select {
	case command := <-s.observer.cancellations:
		s.cancellation = &command
	default:
	}
}

func (s *runObservationSession) cancel(ctx context.Context) bool {
	command := *s.cancellation
	if string(command.JobNamespace) != s.handle.Namespace ||
		(s.handle.UID != "" && command.JobUID != nil && *command.JobUID != s.handle.UID) {
		err := fmt.Errorf("%w: cancellation does not match the worker's pinned Job", runjob.ErrRunJobIdentityMismatch)
		s.controller.runWorkerError(s.handle.RunID, "stop cancellation after Job identity mismatch", err)

		return true
	}
	if s.handle.UID != "" {
		command.JobUID = new(s.handle.UID)
	}
	result, err := s.controller.requestJobCancellation(ctx, command)
	if err != nil {
		return true
	}
	s.handle = result.Handle
	s.cancellation = nil
	if result.JobRetained {
		return false
	}

	s.controller.finishUnstartedCancellation(ctx, s.handle)

	return true
}

func (s *runObservationSession) observe(ctx context.Context) bool {
	observation, err := s.controller.jobs.Observe(ctx, s.handle)
	if ctx.Err() != nil {
		return true
	}
	if err != nil {
		if errors.Is(err, runjob.ErrRunJobIdentityMismatch) {
			s.controller.runWorkerError(s.handle.RunID, "stop observation after Job identity mismatch", err)

			return true
		}
		s.controller.logger.Warn(
			"observe execution Job",
			"run_id", s.handle.RunID,
			"error", err,
		)

		return false
	}

	if observation.JobUID != nil {
		s.handle.UID = *observation.JobUID
	}
	if observation.Terminal() {
		s.controller.finishObservedRun(ctx, s.handle, observation, s.logStream, s.completedLog)

		return true
	}

	s.discardCompletedLog()
	if s.startLog(ctx, observation) {
		return true
	}

	return s.report(ctx, observation)
}

func (s *runObservationSession) discardCompletedLog() {
	if s.completedLog == nil {
		return
	}
	if s.completedLog.err != nil {
		s.controller.logger.Warn(
			"stream Run log",
			"run_id", s.handle.RunID,
			"error", s.completedLog.err,
		)
	}

	s.completedLog = nil
}

func (s *runObservationSession) startLog(ctx context.Context, observation runjob.Observation) bool {
	if observation.Phase != api.RunObservationPhaseRunning || s.logStream != nil {
		return false
	}

	logStream, err := s.controller.startLogSession(ctx, s.handle, true)
	if err != nil {
		if errors.Is(err, runjob.ErrRunJobIdentityMismatch) {
			s.controller.runWorkerError(s.handle.RunID, "retain Job after log source identity mismatch", err)

			return true
		}
		if !errors.Is(err, runjob.ErrLogNotReady) {
			s.controller.logger.Warn(
				"open Run log stream",
				"run_id", s.handle.RunID,
				"error", err,
			)
		}

		return false
	}

	s.logStream = logStream

	return false
}

func (s *runObservationSession) report(ctx context.Context, observation runjob.Observation) bool {
	if !shouldReport(
		observation,
		s.reported,
		s.reportedAt,
		s.controller.now(),
		s.controller.heartbeatEvery,
	) {
		return false
	}

	err := s.controller.reports.ReportObservation(ctx, s.handle.RunID, observationRequest(observation))
	if ctx.Err() != nil {
		return true
	}
	if permanentServerError(err) {
		s.controller.runWorkerError(s.handle.RunID, "report observation", err)

		return true
	}
	if err != nil {
		s.controller.logger.Warn(
			"report Run observation",
			"run_id", s.handle.RunID,
			"phase", observation.Phase,
			"error", err,
		)

		return false
	}

	copy := observation
	s.reported = &copy
	s.reportedAt = s.controller.now()

	return false
}

func (s *runObservationSession) wait(ctx context.Context) bool {
	var logResult <-chan logStreamResult
	if s.logStream != nil {
		logResult = s.logStream.result
	}

	timer := time.NewTimer(s.controller.observeEvery)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return true
	case command := <-s.observer.cancellations:
		s.cancellation = &command
	case result := <-logResult:
		s.logStream = nil
		s.completedLog = &result
	case <-timer.C:
	}

	return false
}

func shouldReport(
	observation runjob.Observation,
	reported *runjob.Observation,
	reportedAt time.Time,
	now time.Time,
	heartbeatEvery time.Duration,
) bool {
	return reported == nil || !sameObservation(observation, *reported) ||
		now.Sub(reportedAt) >= heartbeatEvery
}

func sameObservation(left, right runjob.Observation) bool {
	return left.Phase == right.Phase &&
		equalPointers(left.Outcome, right.Outcome) &&
		equalPointers(left.JobUID, right.JobUID) &&
		equalExecutionImages(left.Images, right.Images) &&
		equalPointers(left.Reason, right.Reason) &&
		equalPointers(left.Message, right.Message) &&
		equalPointers(left.ExitCode, right.ExitCode)
}

func equalExecutionImages(left, right api.ExecutionImages) bool {
	return equalPointers(left.Action, right.Action) &&
		equalPointers(left.Checkout, right.Checkout) &&
		equalPointers(left.Harness, right.Harness)
}

func equalPointers[T comparable](left, right *T) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}
