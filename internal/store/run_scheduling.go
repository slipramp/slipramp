package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ListClusterPlacementRunCounts(
	ctx context.Context,
	clusterName string,
) (map[string]run.RunCounts, error) {
	rows, err := d.queries.ListClusterPlacementRunCounts(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("list Cluster Placement Run counts: %w", err)
	}
	if len(rows) == 0 {
		return nil, cluster.ErrNotFound
	}

	counts := make(map[string]run.RunCounts, len(rows))
	for _, row := range rows {
		if row.PlacementName != "" {
			counts[row.PlacementName] = run.RunCounts{Active: row.ActiveRuns, Queued: row.QueuedRuns}
		}
	}
	return counts, nil
}

func (d *Database) GetRunSchedulingSnapshots(
	ctx context.Context,
	runIDs []uuid.UUID,
	evaluatorProcessID uuid.UUID,
	at time.Time,
	clusterMaximumActiveRuns []byte,
	placementMaximumActiveRuns []byte,
) ([]run.SchedulingSnapshot, error) {
	databaseIDs := make([]pgtype.UUID, 0, len(runIDs))
	for _, runID := range runIDs {
		databaseIDs = append(databaseIDs, databaseUUID(runID))
	}

	rows, err := d.queries.GetRunSchedulingSnapshots(ctx, storedb.GetRunSchedulingSnapshotsParams{
		ClusterMaximumActiveRuns:   clusterMaximumActiveRuns,
		PlacementMaximumActiveRuns: placementMaximumActiveRuns,
		RunIds:                     databaseIDs,
		EvaluatorProcessID:         databaseUUID(evaluatorProcessID),
		CheckedAt:                  databaseTime(at),
	})
	if err != nil {
		return nil, fmt.Errorf("get Run scheduling snapshots: %w", err)
	}

	snapshots := make([]run.SchedulingSnapshot, 0, len(rows))
	for _, row := range rows {
		var eligiblePosition *int32
		if row.EligiblePosition > 0 {
			eligiblePosition = &row.EligiblePosition
		}
		snapshots = append(snapshots, run.SchedulingSnapshot{
			RunID:               uuid.UUID(row.ID.Bytes),
			GatesReady:          row.GatesReady,
			BlockingRunID:       applicationUUID(row.BlockingRunID),
			ClusterActiveRuns:   row.ClusterActiveRuns,
			PlacementActiveRuns: row.PlacementActiveRuns,
			EligiblePosition:    eligiblePosition,
		})
	}

	return snapshots, nil
}
