package controller

import (
	"context"
	"errors"
	"time"

	"github.com/slipramp/slipramp/internal/api"
)

type runObservationSession struct {
	controller   *Controller
	handle       jobHandle
	observer     *runObserver
	cancellation *api.CancelRunCommand
	logStream    *logSession
	completedLog *logStreamResult
	reported     *runObservation
	reportedAt   time.Time
}

func newRunObservationSession(
	controller *Controller,
	handle jobHandle,
	observer *runObserver,
) *runObservationSession {
	return &runObservationSession{
		controller: controller,
		handle:     handle,
		observer:   observer,
	}
}

func (s *runObservationSession) run(ctx context.Context) {
	for {
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
	result, err := s.controller.requestJobCancellation(ctx, *s.cancellation)
	if err != nil {
		return true
	}
	if result.handle.uid != "" {
		s.handle.uid = result.handle.uid
	}
	s.cancellation = nil
	if result.jobRetained {
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
		s.controller.logger.Warn(
			"observe execution Job",
			"run_id", s.handle.runID,
			"error", err,
		)

		return false
	}

	if observation.jobUID != nil {
		s.handle.uid = *observation.jobUID
	}
	if observation.terminal() {
		s.controller.finishObservedRun(ctx, s.handle, observation, s.logStream, s.completedLog)

		return true
	}

	s.discardCompletedLog()
	s.startLog(ctx, observation)

	return s.report(ctx, observation)
}

func (s *runObservationSession) discardCompletedLog() {
	if s.completedLog == nil {
		return
	}
	if s.completedLog.err != nil {
		s.controller.logger.Warn(
			"stream Run log",
			"run_id", s.handle.runID,
			"error", s.completedLog.err,
		)
	}

	s.completedLog = nil
}

func (s *runObservationSession) startLog(ctx context.Context, observation runObservation) {
	if observation.phase != api.RunObservationPhaseRunning || s.logStream != nil {
		return
	}

	logStream, err := s.controller.startLogSession(ctx, s.handle, true)
	if err != nil {
		if !errors.Is(err, errLogNotReady) {
			s.controller.logger.Warn(
				"open Run log stream",
				"run_id", s.handle.runID,
				"error", err,
			)
		}

		return
	}

	s.logStream = logStream
}

func (s *runObservationSession) report(ctx context.Context, observation runObservation) bool {
	if !shouldReport(
		observation,
		s.reported,
		s.reportedAt,
		s.controller.now(),
		s.controller.heartbeatEvery,
	) {
		return false
	}

	err := s.controller.reports.ReportObservation(ctx, s.handle.runID, observationRequest(observation))
	if ctx.Err() != nil {
		return true
	}
	if errors.Is(err, errRunObservationRejected) {
		s.controller.logRunRejection(s.handle.runID, "report observation", err)

		return true
	}
	if err != nil {
		s.controller.logger.Warn(
			"report Run observation",
			"run_id", s.handle.runID,
			"phase", observation.phase,
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
	observation runObservation,
	reported *runObservation,
	reportedAt time.Time,
	now time.Time,
	heartbeatEvery time.Duration,
) bool {
	return reported == nil || !sameObservation(observation, *reported) ||
		now.Sub(reportedAt) >= heartbeatEvery
}

func sameObservation(left, right runObservation) bool {
	return left.phase == right.phase &&
		equalPointers(left.outcome, right.outcome) &&
		equalPointers(left.jobUID, right.jobUID) &&
		equalPointers(left.reason, right.reason) &&
		equalPointers(left.message, right.message) &&
		equalPointers(left.exitCode, right.exitCode)
}

func equalPointers[T comparable](left, right *T) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}
