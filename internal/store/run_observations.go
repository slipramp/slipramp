package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ApplyRunObservation(ctx context.Context, observation run.Observation) error {
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

	state := run.ObservationTransitionState{
		Phase:                 run.Phase(current.Phase),
		Outcome:               runOutcome(current.Outcome),
		JobUID:                current.JobUid,
		CancellationRequested: current.CancellationRequestedAt.Valid,
	}
	if err := run.ValidateObservationTransition(state, observation); err != nil {
		return err
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

	phaseChanged := state.Phase != observation.Phase
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
	if current.ActionName == "sync" && phaseChanged && observation.Phase == run.PhaseFinished &&
		observation.Outcome != nil && *observation.Outcome == run.OutcomeSucceeded {
		updated, err := queries.RecordTargetSyncSuccess(
			ctx,
			storedb.RecordTargetSyncSuccessParams{
				RunID:    databaseUUID(observation.ID),
				TargetID: current.TargetID,
			},
		)
		if err != nil {
			return fmt.Errorf("record Target sync success: %w", err)
		}
		if updated != 1 {
			return errors.New("record Target sync success: Target does not exist")
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Run observation transaction: %w", err)
	}

	return nil
}

func outcomeString(value *run.Outcome) *string {
	if value == nil {
		return nil
	}

	converted := string(*value)

	return &converted
}
