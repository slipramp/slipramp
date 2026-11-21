package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/scheduletrigger"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ListScheduledTriggers(ctx context.Context) ([]scheduletrigger.Definition, error) {
	rows, err := d.queries.ListScheduledTriggers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active scheduled triggers: %w", err)
	}

	definitions := make([]scheduletrigger.Definition, 0, len(rows))
	for _, row := range rows {
		var trigger catalog.Trigger
		if err := json.Unmarshal(row.TriggerDefinition, &trigger); err != nil {
			return nil, fmt.Errorf("decode scheduled trigger: %w", err)
		}

		definitions = append(definitions, scheduletrigger.Definition{
			TargetID:          uuid.UUID(row.TargetID.Bytes),
			CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
			ActionName:        row.ActionName,
			TriggerName:       trigger.Name,
			Cron:              trigger.Schedule.Cron,
			Parameters:        trigger.Parameters,
		})
	}
	return definitions, nil
}

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
