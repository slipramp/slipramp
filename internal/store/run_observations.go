package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ApplyRunObservation(
	ctx context.Context,
	observation run.Observation,
	allowedCurrentPhases []run.Phase,
) error {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Run observation transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	current, err := queries.GetRunObservationStateForUpdate(
		ctx,
		storedb.GetRunObservationStateForUpdateParams{
			ID:          databaseUUID(observation.ID),
			ClusterName: observation.ClusterName,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ErrRunNotFound
	}
	if err != nil {
		return fmt.Errorf("get current Run phase: %w", err)
	}

	currentPhase := run.Phase(current.Phase)
	if !slices.Contains(allowedCurrentPhases, currentPhase) ||
		!sameJob(current.JobUid, observation.JobUID) ||
		!sameFinishedOutcome(currentPhase, current.Outcome, observation.Outcome) ||
		!validCanceledObservation(current.CancellationRequestedAt.Valid, observation.Outcome) {
		return run.ErrObservationConflict
	}

	if err := queries.UpdateRunObservation(
		ctx,
		storedb.UpdateRunObservationParams{
			Phase:      string(observation.Phase),
			Outcome:    outcomeString(observation.Outcome),
			JobUid:     observation.JobUID,
			ObservedAt: databaseTime(observation.ObservedAt),
			Reason:     observation.Reason,
			Message:    observation.Message,
			ExitCode:   observation.ExitCode,
			ID:         databaseUUID(observation.ID),
		},
	); err != nil {
		return fmt.Errorf("update Run observation: %w", err)
	}

	phaseChanged := currentPhase != observation.Phase
	if phaseChanged {
		err = queries.CreateRunPhaseTransition(
			ctx,
			storedb.CreateRunPhaseTransitionParams{
				RunID:      databaseUUID(observation.ID),
				Phase:      string(observation.Phase),
				Outcome:    outcomeString(observation.Outcome),
				Reason:     observation.Reason,
				Message:    observation.Message,
				ExitCode:   observation.ExitCode,
				RecordedAt: databaseTime(observation.ObservedAt),
			},
		)
		if err != nil {
			return fmt.Errorf("record Run phase transition: %w", err)
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Run observation transaction: %w", err)
	}

	return nil
}

func sameJob(current, observed *string) bool {
	return current == nil || observed != nil && *current == *observed
}

func sameFinishedOutcome(currentPhase run.Phase, currentOutcome *string, observedOutcome *run.Outcome) bool {
	if currentPhase != run.PhaseFinished {
		return true
	}

	return currentOutcome != nil && observedOutcome != nil &&
		*currentOutcome == string(*observedOutcome)
}

func validCanceledObservation(cancellationRequested bool, observedOutcome *run.Outcome) bool {
	return observedOutcome == nil || *observedOutcome != run.OutcomeCanceled ||
		cancellationRequested
}

func outcomeString(value *run.Outcome) *string {
	if value == nil {
		return nil
	}

	converted := string(*value)

	return &converted
}
