package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) DecideRunReview(ctx context.Context, transition run.ReviewTransition) (run.Run, error) {
	decidedByPrincipalID, decidedBySnapshot, err := actorDatabaseFields(&transition.DecidedBy)
	if err != nil {
		return run.Run{}, fmt.Errorf("encode Run review actor: %w", err)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin Run review decision: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	current, err := queries.GetRunForReview(ctx, databaseUUID(transition.RunID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrRunNotFound
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get Run for review: %w", err)
	}

	currentRecord, err := runFromRow(current)
	if err != nil {
		return run.Run{}, err
	}
	state := run.ReviewTransitionState{Phase: currentRecord.Phase, Review: currentRecord.Review}
	if err := run.ValidateReviewTransition(state, transition); err != nil {
		return run.Run{}, err
	}

	if currentRecord.Review.State == run.ReviewStateApproved ||
		currentRecord.Review.State == run.ReviewStateRejected {
		return commitRunReview(ctx, transaction, current)
	}

	updated, err := queries.DecideRunReview(ctx, storedb.DecideRunReviewParams{
		ReviewState:                string(transition.Decision),
		ReviewDecidedByPrincipalID: decidedByPrincipalID,
		ReviewDecidedBySnapshot:    decidedBySnapshot,
		ReviewDecidedAt:            databaseTime(transition.DecidedAt),
		ID:                         databaseUUID(transition.RunID),
	})
	if err != nil {
		return run.Run{}, fmt.Errorf("decide Run review: %w", err)
	}

	if transition.Decision == run.ReviewDecisionRejected {
		outcome := string(run.OutcomeCanceled)
		reason := run.ReasonReviewRejected
		if err := queries.CreateRunPhaseTransition(ctx, storedb.CreateRunPhaseTransitionParams{
			RunID:      databaseUUID(transition.RunID),
			Phase:      string(run.PhaseFinished),
			Outcome:    &outcome,
			Reason:     &reason,
			RecordedAt: databaseTime(transition.DecidedAt),
		}); err != nil {
			return run.Run{}, fmt.Errorf("record rejected Run phase: %w", err)
		}
	}

	return commitRunReview(ctx, transaction, updated)
}

func commitRunReview(ctx context.Context, transaction pgx.Tx, row storedb.Run) (run.Run, error) {
	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit Run review decision: %w", err)
	}

	return runFromRow(row)
}
