package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ gittrigger.Store = (*Database)(nil)

func createGitTriggerDeliveries(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	revisionID pgtype.UUID,
	result repository.RefreshResult,
) error {
	for _, target := range result.Revision.Catalog.Targets {
		if !catalog.TargetAffected(target, result.ChangedPaths) {
			continue
		}

		for _, actionName := range slices.Sorted(maps.Keys(target.Spec.Actions)) {
			action := target.Spec.Actions[actionName]
			for _, trigger := range action.Triggers {
				if trigger.Git == nil {
					continue
				}

				parameters, err := json.Marshal(trigger.Parameters)
				if err != nil {
					return fmt.Errorf(
						"marshal Git-trigger delivery parameters for target %q action %q trigger %q: %w",
						target.Metadata.Name,
						actionName,
						trigger.Name,
						err,
					)
				}

				err = queries.CreateGitTriggerDelivery(
					ctx,
					storedb.CreateGitTriggerDeliveryParams{
						ID:                databaseUUID(uuid.New()),
						RepositoryID:      repositoryID,
						CatalogRevisionID: revisionID,
						TargetName:        target.Metadata.Name,
						ActionName:        actionName,
						TriggerName:       trigger.Name,
						Parameters:        parameters,
						CreatedAt:         databaseTime(result.AttemptedAt),
					},
				)
				if err != nil {
					return fmt.Errorf(
						"create Git-trigger delivery for target %q action %q trigger %q: %w",
						target.Metadata.Name,
						actionName,
						trigger.Name,
						err,
					)
				}
			}
		}
	}

	return nil
}

func coalesceSupersededGitWork(
	ctx context.Context,
	queries *storedb.Queries,
	repositoryID pgtype.UUID,
	targets []targetStateChange,
	finishedAt time.Time,
) error {
	for _, target := range targets {
		if err := queries.SupersedePendingGitTriggerDeliveries(
			ctx,
			storedb.SupersedePendingGitTriggerDeliveriesParams{
				SupersededAt: databaseTime(finishedAt),
				RepositoryID: repositoryID,
				TargetName:   target.name,
			},
		); err != nil {
			return fmt.Errorf("supersede pending Git-trigger deliveries for target %q: %w", target.name, err)
		}

		finishedRunIDs, err := queries.FinishSupersededQueuedGitRuns(
			ctx,
			storedb.FinishSupersededQueuedGitRunsParams{
				FinishedAt:       databaseTime(finishedAt),
				RepositoryID:     repositoryID,
				TargetName:       target.name,
				DesiredCommitSha: target.desiredCommitSHA,
				CancelAll:        target.archived,
			},
		)
		if err != nil {
			return fmt.Errorf("finish superseded queued Git Runs for target %q: %w", target.name, err)
		}

		outcome := string(run.OutcomeCanceled)
		reason := run.ReasonSupersededByGitChange
		for _, runID := range finishedRunIDs {
			if err := queries.CreateRunPhaseTransition(
				ctx,
				storedb.CreateRunPhaseTransitionParams{
					RunID:      runID,
					Phase:      string(run.PhaseFinished),
					Outcome:    &outcome,
					Reason:     &reason,
					RecordedAt: databaseTime(finishedAt),
				},
			); err != nil {
				return fmt.Errorf("record superseded Run %s phase transition: %w", uuid.UUID(runID.Bytes), err)
			}
		}
	}

	return nil
}

func supersedeStaleGitRun(
	ctx context.Context,
	queries *storedb.Queries,
	record run.Run,
) (bool, error) {
	if record.Source.Git == nil {
		return false, errors.New("git-triggered Run source is missing delivery details")
	}

	_, err := queries.LockActiveRepository(ctx, databaseUUID(record.RepositoryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return markGitTriggerDeliverySuperseded(ctx, queries, record)
	}
	if err != nil {
		return false, fmt.Errorf("lock Git-triggered Run repository: %w", err)
	}

	targetState, err := queries.LockTargetStateForGitRun(
		ctx,
		storedb.LockTargetStateForGitRunParams{
			RepositoryID: databaseUUID(record.RepositoryID),
			TargetName:   record.TargetName,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return markGitTriggerDeliverySuperseded(ctx, queries, record)
	}
	if err != nil {
		return false, fmt.Errorf("lock Git-triggered Run target state: %w", err)
	}
	if targetState.ArchivedAt.Valid || targetState.DesiredCommitSha != record.CommitSHA {
		return markGitTriggerDeliverySuperseded(ctx, queries, record)
	}

	return false, nil
}

func markGitTriggerDeliverySuperseded(
	ctx context.Context,
	queries *storedb.Queries,
	record run.Run,
) (bool, error) {
	if _, err := queries.SupersedeGitTriggerDelivery(
		ctx,
		storedb.SupersedeGitTriggerDeliveryParams{
			SupersededAt: databaseTime(record.CreatedAt),
			ID:           databaseUUID(record.Source.Git.DeliveryID),
		},
	); err != nil {
		return false, fmt.Errorf("supersede stale Git-trigger delivery: %w", err)
	}

	return true, nil
}

func (d *Database) GetPendingGitTriggerDelivery(ctx context.Context) (gittrigger.Delivery, error) {
	row, err := d.queries.GetPendingGitTriggerDelivery(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return gittrigger.Delivery{}, gittrigger.ErrNoPendingDelivery
	}
	if err != nil {
		return gittrigger.Delivery{}, fmt.Errorf("get pending Git-trigger delivery: %w", err)
	}

	delivery, err := gitTriggerDeliveryFromRow(row)
	if err != nil {
		return gittrigger.Delivery{}, err
	}

	return delivery, nil
}

func (d *Database) CompleteGitTriggerDelivery(ctx context.Context, deliveryID uuid.UUID, runID uuid.UUID) error {
	rowsAffected, err := d.queries.CompleteGitTriggerDelivery(
		ctx,
		storedb.CompleteGitTriggerDeliveryParams{
			ID:    databaseUUID(deliveryID),
			RunID: databaseUUID(runID),
		},
	)
	if err != nil {
		return fmt.Errorf("complete Git-trigger delivery: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("complete Git-trigger delivery %s: delivery does not exist or has another Run", deliveryID)
	}

	return nil
}

func gitTriggerDeliveryFromRow(row storedb.GitTriggerDelivery) (gittrigger.Delivery, error) {
	var parameters map[string]any
	if err := json.Unmarshal(row.Parameters, &parameters); err != nil {
		return gittrigger.Delivery{}, fmt.Errorf("decode Git-trigger delivery parameters: %w", err)
	}

	return gittrigger.Delivery{
		ID:                uuid.UUID(row.ID.Bytes),
		RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
		CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
		TargetName:        row.TargetName,
		ActionName:        row.ActionName,
		TriggerName:       row.TriggerName,
		Parameters:        parameters,
	}, nil
}
