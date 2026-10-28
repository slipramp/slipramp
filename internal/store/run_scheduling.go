package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) GetRunSchedulingSnapshots(
	ctx context.Context,
	runIDs []uuid.UUID,
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
			BlockingRunID:       applicationUUID(row.BlockingRunID),
			ClusterActiveRuns:   row.ClusterActiveRuns,
			PlacementActiveRuns: row.PlacementActiveRuns,
			EligiblePosition:    eligiblePosition,
		})
	}

	return snapshots, nil
}
