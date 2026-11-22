package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) GetAssignedRunForCluster(ctx context.Context, clusterName string) (run.Run, error) {
	row, err := d.queries.GetAssignedRunForCluster(ctx, clusterName)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrNoRunAssignment
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get assigned Run for cluster: %w", err)
	}

	record, err := runFromRow(row)
	if err != nil {
		return run.Run{}, err
	}

	return record, nil
}

func (d *Database) ClaimQueuedRun(
	ctx context.Context,
	clusterName string,
	capacity run.SchedulingCapacity,
	gateEvaluatorProcessID uuid.UUID,
) (run.Run, error) {
	return d.claimQueuedRun(ctx, clusterName, capacity, gateEvaluatorProcessID, (*storedb.Queries).ServerGateCheckTime)
}

func (d *Database) claimQueuedRun(
	ctx context.Context,
	clusterName string,
	capacity run.SchedulingCapacity,
	gateEvaluatorProcessID uuid.UUID,
	checkTime gateCheckClock,
) (run.Run, error) {
	placementMaximumActiveRuns, err := json.Marshal(capacity.PlacementMaximumActiveRuns)
	if err != nil {
		return run.Run{}, fmt.Errorf("encode Placement Run capacities: %w", err)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("begin Run claim transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	initial, err := checkTime(queries, ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("read Run selection time: %w", err)
	}
	queue := storedb.LockRunAssignmentCandidateParams{
		ClusterName:                clusterName,
		ClusterMaximumActiveRuns:   capacity.ClusterMaximumActiveRuns,
		PlacementMaximumActiveRuns: placementMaximumActiveRuns,
		EvaluatorProcessID:         databaseUUID(gateEvaluatorProcessID),
		CheckedAt:                  initial,
	}
	candidate, err := queries.LockRunAssignmentCandidate(ctx, queue)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Run{}, run.ErrNoRunAssignment
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("select Run for assignment: %w", err)
	}
	record, err := runFromRow(candidate)
	if err != nil {
		return run.Run{}, err
	}

	mutating := record.Snapshot.Action.Intent != catalog.ActionIntentPreview
	var scopes []storedb.ServerGateScope
	if mutating {
		scopes, err = queries.ShareRunGateScopes(ctx, candidate.TargetID)
		if err != nil {
			return run.Run{}, fmt.Errorf("lock Run Gate scopes: %w", err)
		}
	}
	if len(record.Snapshot.Action.Gates) > 0 {
		if _, err := queries.LockRunGateRepository(ctx, candidate.RepositoryID); errors.Is(err, pgx.ErrNoRows) {
			return run.Run{}, run.ErrNoRunAssignment
		} else if err != nil {
			return run.Run{}, fmt.Errorf("lock Run Gate Repository: %w", err)
		}
	}
	// Sample after acquiring the policy locks. A wait can outlive a window
	// observation, a lock, or a repository evaluator's passing observation.
	assignedAt, err := checkTime(queries, ctx)
	if err != nil {
		return run.Run{}, fmt.Errorf("read Run release time: %w", err)
	}
	var evidence []byte
	if mutating {
		evidence, err = freezeServerGates(ctx, queries, gate.RunSubject, record.ID, scopes, assignedAt.Time.UTC())
		if err != nil {
			return run.Run{}, err
		}
		if evidence == nil {
			return run.Run{}, run.ErrNoRunAssignment
		}
	}
	updated, err := queries.AssignEligibleRun(ctx, storedb.AssignEligibleRunParams{
		ID:                         candidate.ID,
		CheckedAt:                  assignedAt,
		Evidence:                   evidence,
		ClusterName:                queue.ClusterName,
		ClusterMaximumActiveRuns:   queue.ClusterMaximumActiveRuns,
		PlacementMaximumActiveRuns: queue.PlacementMaximumActiveRuns,
		EvaluatorProcessID:         queue.EvaluatorProcessID,
	})
	if err != nil {
		return run.Run{}, fmt.Errorf("assign eligible Run: %w", err)
	}
	if updated != 1 {
		return run.Run{}, run.ErrNoRunAssignment
	}

	released, err := queries.ReleaseRunGates(ctx, storedb.ReleaseRunGatesParams{
		RunID:              candidate.ID,
		EvaluatorProcessID: databaseUUID(gateEvaluatorProcessID),
		ReleasedAt:         assignedAt,
	})
	if err != nil {
		return run.Run{}, fmt.Errorf("release Run Gates: %w", err)
	}
	if released != int64(len(record.Snapshot.Action.Gates)) {
		return run.Run{}, run.ErrNoRunAssignment
	}

	if candidate.ActionName == "sync" {
		updated, err := queries.RecordTargetSyncAttempt(
			ctx,
			storedb.RecordTargetSyncAttemptParams{
				RunID:    candidate.ID,
				TargetID: candidate.TargetID,
			},
		)
		if err != nil {
			return run.Run{}, fmt.Errorf("record Target sync attempt: %w", err)
		}
		if updated != 1 {
			return run.Run{}, errors.New("record Target sync attempt: Target does not exist")
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return run.Run{}, fmt.Errorf("commit Run claim transaction: %w", err)
	}

	record.Phase = run.PhaseAssigned
	record.AssignedAt = applicationTime(assignedAt)
	return record, nil
}
