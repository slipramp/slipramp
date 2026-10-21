package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func (s *Service) ReportObservation(ctx context.Context, observation Observation) error {
	if observation.ID == uuid.Nil {
		return fmt.Errorf("%w: Run ID is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(observation.ClusterName) == "" {
		return fmt.Errorf("%w: cluster name is required", ErrInvalidRequest)
	}

	allowedCurrentPhases, err := validateObservation(observation)
	if err != nil {
		return err
	}

	observation.ObservedAt = s.now().UTC()
	err = s.observations.ApplyRunObservation(ctx, observation, allowedCurrentPhases)
	if err != nil {
		switch {
		case errors.Is(err, ErrRunNotFound),
			errors.Is(err, ErrObservationConflict):
			return err
		default:
			return fmt.Errorf("record Run observation: %w", err)
		}
	}

	return nil
}

func validateObservation(observation Observation) ([]Phase, error) {
	stringsToValidate := []struct {
		name  string
		value *string
	}{
		{name: "Job UID", value: observation.JobUID},
		{name: "reason", value: observation.Reason},
		{name: "message", value: observation.Message},
	}
	for _, field := range stringsToValidate {
		if field.value != nil && strings.TrimSpace(*field.value) == "" {
			return nil, fmt.Errorf("%w: %s must not be blank", ErrInvalidRequest, field.name)
		}
	}
	if observation.ExitCode != nil && *observation.ExitCode < 0 {
		return nil, fmt.Errorf("%w: exit code must not be negative", ErrInvalidRequest)
	}

	switch observation.Phase {
	case PhasePreparing:
		if hasTerminalDetails(observation) {
			return nil, fmt.Errorf("%w: preparing Run must not have terminal details", ErrInvalidRequest)
		}
		if observation.JobUID == nil {
			return nil, fmt.Errorf("%w: preparing Run requires a Job UID", ErrInvalidRequest)
		}

		return []Phase{PhaseAssigned, PhasePreparing}, nil
	case PhaseRunning:
		if hasTerminalDetails(observation) {
			return nil, fmt.Errorf("%w: running Run must not have terminal details", ErrInvalidRequest)
		}
		if observation.JobUID == nil {
			return nil, fmt.Errorf("%w: running Run requires a Job UID", ErrInvalidRequest)
		}

		return []Phase{PhaseAssigned, PhasePreparing, PhaseRunning}, nil
	case PhaseFinished:
		if observation.Outcome == nil || !validOutcome(*observation.Outcome) {
			return nil, fmt.Errorf("%w: finished Run requires a valid outcome", ErrInvalidRequest)
		}

		return []Phase{
			PhaseAssigned,
			PhasePreparing,
			PhaseRunning,
			PhaseFinished,
		}, nil
	default:
		return nil, fmt.Errorf("%w: controller cannot report Run phase %q", ErrInvalidRequest, observation.Phase)
	}
}

func hasTerminalDetails(observation Observation) bool {
	return observation.Outcome != nil || observation.Reason != nil ||
		observation.Message != nil || observation.ExitCode != nil
}

func validOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeSucceeded, OutcomeFailed, OutcomeCanceled, OutcomeTimedOut:
		return true
	default:
		return false
	}
}
