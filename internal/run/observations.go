package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ObservationStore interface {
	ApplyRunObservation(ctx context.Context, observation Observation) error
}

// ObservationTransitionState contains the persisted facts needed to accept an
// observation. Storage must read this state and validate the transition under
// the same lock used to update the Run.
type ObservationTransitionState struct {
	Phase                   Phase
	Outcome                 *Outcome
	OutcomeReason           *string
	CreatedAt               time.Time
	FinishedAt              *time.Time
	CancellationRequestedAt *time.Time
	CanceledByPresent       bool
	JobUID                  *string
	Images                  ExecutionImages
}

func (s *Service) ReportObservation(ctx context.Context, observation Observation) error {
	if observation.ID == uuid.Nil {
		return fmt.Errorf("%w: Run ID is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(observation.ClusterName) == "" {
		return fmt.Errorf("%w: cluster name is required", ErrInvalidRequest)
	}

	if err := validateObservation(observation); err != nil {
		return err
	}

	observation.ObservedAt = s.now().UTC()
	err := s.observations.ApplyRunObservation(ctx, observation)
	if err != nil {
		switch {
		case errors.Is(err, ErrRunNotFound),
			errors.Is(err, ErrObservationConflict):
			return err
		default:
			return fmt.Errorf("record Run observation: %w", err)
		}
	}
	s.notifyControllerCluster(observation.ClusterName)

	return nil
}

// ValidateObservationTransition checks the observation's lifecycle fields
// against the persisted Run state without changing either value.
func ValidateObservationTransition(current ObservationTransitionState, observation Observation) error {
	if err := validateObservation(observation); err != nil {
		return err
	}
	cancellation := current.cancellationStatus()
	cancellationState, err := cancellation.CancellationState()
	if err != nil {
		return ErrObservationConflict
	}
	if !validObservationPhaseTransition(current.Phase, observation.Phase) ||
		!sameObservationJob(current.JobUID, observation.JobUID) ||
		!sameExecutionImages(current.Images, observation.Images) ||
		!sameFinishedOutcome(current, observation.Outcome) {
		return ErrObservationConflict
	}
	if observation.Outcome != nil && *observation.Outcome == OutcomeCanceled {
		if (cancellationState != CancellationStateRequested &&
			cancellationState != CancellationStateCompleted) ||
			!reasonIs(observation.Reason, ReasonCancelRequested) {
			return ErrObservationConflict
		}
	}

	nextCancellation := cancellation
	nextCancellation.Phase = observation.Phase
	nextCancellation.Outcome = observation.Outcome
	if current.Phase != PhaseFinished && observation.Phase == PhaseFinished {
		nextCancellation.OutcomeReason = observation.Reason
		if !observation.ObservedAt.IsZero() {
			nextCancellation.FinishedAt = &observation.ObservedAt
		}
	}
	if err := ValidateCancellationTransition(cancellation, nextCancellation); err != nil {
		return ErrObservationConflict
	}

	return nil
}

func (s ObservationTransitionState) cancellationStatus() CancellationStatus {
	return CancellationStatus{
		Phase:                   s.Phase,
		Outcome:                 s.Outcome,
		OutcomeReason:           s.OutcomeReason,
		CreatedAt:               s.CreatedAt,
		FinishedAt:              s.FinishedAt,
		CancellationRequestedAt: s.CancellationRequestedAt,
		CanceledByPresent:       s.CanceledByPresent,
	}
}

func validateObservation(observation Observation) error {
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
			return fmt.Errorf("%w: %s must not be blank", ErrInvalidRequest, field.name)
		}
	}
	if observation.ExitCode != nil && *observation.ExitCode < 0 {
		return fmt.Errorf("%w: exit code must not be negative", ErrInvalidRequest)
	}
	images := []struct {
		name  string
		value *string
	}{
		{name: "Action image ID", value: observation.Images.Action},
		{name: "checkout image ID", value: observation.Images.Checkout},
		{name: "Harness image ID", value: observation.Images.Harness},
	}
	for _, image := range images {
		if image.value == nil {
			continue
		}
		if strings.TrimSpace(*image.value) == "" {
			return fmt.Errorf("%w: %s must not be blank", ErrInvalidRequest, image.name)
		}
		if len(*image.value) > MaximumImageIDLength {
			return fmt.Errorf(
				"%w: %s must not exceed %d bytes",
				ErrInvalidRequest,
				image.name,
				MaximumImageIDLength,
			)
		}
	}

	switch observation.Phase {
	case PhasePreparing:
		if hasTerminalDetails(observation) {
			return fmt.Errorf("%w: preparing Run must not have terminal details", ErrInvalidRequest)
		}
		if observation.JobUID == nil {
			return fmt.Errorf("%w: preparing Run requires a Job UID", ErrInvalidRequest)
		}
	case PhaseRunning:
		if hasTerminalDetails(observation) {
			return fmt.Errorf("%w: running Run must not have terminal details", ErrInvalidRequest)
		}
		if observation.JobUID == nil {
			return fmt.Errorf("%w: running Run requires a Job UID", ErrInvalidRequest)
		}
	case PhaseFinished:
		if observation.Outcome == nil || !validOutcome(*observation.Outcome) {
			return fmt.Errorf("%w: finished Run requires a valid outcome", ErrInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: controller cannot report Run phase %q", ErrInvalidRequest, observation.Phase)
	}

	return nil
}

func validObservationPhaseTransition(current, observed Phase) bool {
	switch observed {
	case PhasePreparing:
		return current == PhaseAssigned || current == PhasePreparing
	case PhaseRunning:
		return current == PhaseAssigned || current == PhasePreparing || current == PhaseRunning
	case PhaseFinished:
		return current == PhaseAssigned || current == PhasePreparing || current == PhaseRunning || current == PhaseFinished
	default:
		return false
	}
}

func sameObservationJob(current, observed *string) bool {
	return current == nil || observed != nil && *current == *observed
}

func sameExecutionImages(current, observed ExecutionImages) bool {
	return sameOptionalObservationValue(current.Action, observed.Action) &&
		sameOptionalObservationValue(current.Checkout, observed.Checkout) &&
		sameOptionalObservationValue(current.Harness, observed.Harness)
}

func sameOptionalObservationValue(current, observed *string) bool {
	return current == nil || observed == nil || *current == *observed
}

func sameFinishedOutcome(current ObservationTransitionState, observed *Outcome) bool {
	if current.Phase != PhaseFinished {
		return true
	}

	return current.Outcome != nil && observed != nil && *current.Outcome == *observed
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
