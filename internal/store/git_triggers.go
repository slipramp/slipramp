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

func (d *Database) GetEligibleGitTriggerDelivery(ctx context.Context, now time.Time) (gittrigger.Delivery, error) {
	row, err := d.queries.GetEligibleGitTriggerDelivery(ctx, databaseTime(now))
	if errors.Is(err, pgx.ErrNoRows) {
		return gittrigger.Delivery{}, gittrigger.ErrNoEligibleDelivery
	}
	if err != nil {
		return gittrigger.Delivery{}, fmt.Errorf("get eligible Git-trigger delivery: %w", err)
	}

	delivery, err := gitTriggerDeliveryFromRow(row)
	if err != nil {
		return gittrigger.Delivery{}, err
	}

	return delivery, nil
}

func (d *Database) RecordGitTriggerDeliveryFailure(
	ctx context.Context, deliveryID uuid.UUID, now time.Time, diagnostic string,
) (gittrigger.FailureState, bool, error) {
	row, err := d.queries.RecordGitTriggerDeliveryFailure(ctx, storedb.RecordGitTriggerDeliveryFailureParams{
		ID:                  databaseUUID(deliveryID),
		AttemptedAt:         databaseTime(now),
		Diagnostic:          &diagnostic,
		FailureLimit:        gittrigger.FailureLimit,
		InitialDelaySeconds: gittrigger.InitialRetryDelay.Seconds(),
		MaxDelaySeconds:     gittrigger.MaximumRetryDelay.Seconds(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return gittrigger.FailureState{}, false, nil
	}
	if err != nil {
		return gittrigger.FailureState{}, false, fmt.Errorf("record Git-trigger delivery failure: %w", err)
	}
	return gittrigger.FailureState{
		Count:    int(row.FailureCount),
		Terminal: row.FailedAt.Valid,
	}, true, nil
}

func linkGitTriggerRun(ctx context.Context, queries *storedb.Queries, record run.Run) error {
	if record.Source.Type != run.SourceTypeGit {
		return nil
	}
	if record.Source.Git == nil || record.IdempotencyKey == nil ||
		*record.IdempotencyKey != record.Source.Git.DeliveryID.String() {
		return errors.New("git Run must use its delivery identity")
	}
	affected, err := queries.LinkGitTriggerRun(ctx, storedb.LinkGitTriggerRunParams{
		ID:                databaseUUID(record.Source.Git.DeliveryID),
		RunID:             databaseUUID(record.ID),
		RepositoryID:      databaseUUID(record.RepositoryID),
		CatalogRevisionID: databaseUUID(record.CatalogRevisionID),
		TargetName:        record.TargetName,
		ActionName:        record.ActionName,
		TriggerName:       record.Source.Git.TriggerName,
		AttemptedAt:       databaseTime(record.CreatedAt),
	})
	if err != nil {
		return fmt.Errorf("link Git-trigger Run: %w", err)
	}
	if affected != 1 {
		return errors.New("git-trigger delivery is not eligible or has a different Run or definition")
	}
	return nil
}

func (d *Database) ListFailedGitTriggerDeliveries(
	ctx context.Context, limit int, offset int,
) (gittrigger.FailedDeliveryPage, error) {
	if limit < 1 || limit > 200 || offset < 0 || offset > 2147483647 {
		return gittrigger.FailedDeliveryPage{}, errors.New("invalid failed delivery list window")
	}
	rows, err := d.queries.ListFailedGitTriggerDeliveries(ctx, storedb.ListFailedGitTriggerDeliveriesParams{
		ResultLimit:  int32(limit),
		ResultOffset: int32(offset),
	})
	if err != nil {
		return gittrigger.FailedDeliveryPage{}, fmt.Errorf("list failed Git-trigger deliveries: %w", err)
	}
	total, err := d.queries.CountFailedGitTriggerDeliveries(ctx)
	if err != nil {
		return gittrigger.FailedDeliveryPage{}, fmt.Errorf("count failed Git-trigger deliveries: %w", err)
	}
	page := gittrigger.FailedDeliveryPage{
		Items: make([]gittrigger.FailedDelivery, 0, len(rows)),
		Total: int(total),
	}
	for _, row := range rows {
		diagnostic := ""
		if row.Diagnostic != nil {
			diagnostic = *row.Diagnostic
		}
		page.Items = append(page.Items, gittrigger.FailedDelivery{
			ID:                uuid.UUID(row.ID.Bytes),
			RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
			RepositoryName:    row.RepositoryName,
			CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
			CommitSHA:         row.CommitSha,
			TargetName:        row.TargetName,
			ActionName:        row.ActionName,
			TriggerName:       row.TriggerName,
			FailureCount:      int(row.FailureCount),
			Diagnostic:        diagnostic,
			FailedAt:          row.FailedAt.Time.UTC(),
		})
	}
	return page, nil
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
