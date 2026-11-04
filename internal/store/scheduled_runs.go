package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func rejectScheduledRunOverlap(ctx context.Context, queries *storedb.Queries, record run.Run) error {
	if record.Source.Type != run.SourceTypeSchedule {
		return nil
	}
	if record.Source.Schedule == nil {
		return fmt.Errorf("%w: schedule source details are required", run.ErrInvalidRequest)
	}

	if _, err := queries.LockTargetForScheduledRun(ctx, databaseUUID(record.TargetID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return run.ErrTargetNotFound
		}

		return fmt.Errorf("lock scheduled Run Target: %w", err)
	}
	overlaps, err := queries.HasUnfinishedScheduledRun(ctx, storedb.HasUnfinishedScheduledRunParams{
		TargetID:    databaseUUID(record.TargetID),
		ActionName:  record.ActionName,
		TriggerName: record.Source.Schedule.TriggerName,
	})
	if err != nil {
		return fmt.Errorf("check scheduled Run overlap: %w", err)
	}
	if overlaps {
		return run.ErrScheduleOverlap
	}

	return nil
}
