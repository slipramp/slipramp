package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/inbox"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ListInboxProblemAcknowledgements(
	ctx context.Context,
	principalID uuid.UUID,
) ([]string, error) {
	if principalID == uuid.Nil {
		return nil, fmt.Errorf("%w: principal ID is required", productidentity.ErrInvalid)
	}

	problemIDs, err := d.queries.ListInboxProblemAcknowledgements(ctx, databaseUUID(principalID))
	if err != nil {
		return nil, fmt.Errorf("list Inbox problem acknowledgements: %w", err)
	}

	return problemIDs, nil
}

func (d *Database) InsertInboxProblemAcknowledgements(
	ctx context.Context,
	principalID uuid.UUID,
	problemIDs []string,
	acknowledgedAt time.Time,
) error {
	if principalID == uuid.Nil {
		return fmt.Errorf("%w: principal ID is required", productidentity.ErrInvalid)
	}
	if acknowledgedAt.IsZero() {
		return fmt.Errorf("%w: acknowledgement time is required", productidentity.ErrInvalid)
	}

	err := d.queries.InsertInboxProblemAcknowledgements(
		ctx,
		storedb.InsertInboxProblemAcknowledgementsParams{
			PrincipalID:    databaseUUID(principalID),
			AcknowledgedAt: databaseTime(acknowledgedAt),
			ProblemIds:     problemIDs,
		},
	)
	if err != nil {
		return fmt.Errorf("insert Inbox problem acknowledgements: %w", err)
	}

	return nil
}

func (d *Database) ListInboxActiveRuns(ctx context.Context) ([]inbox.ActiveRun, error) {
	rows, err := d.queries.ListInboxActiveRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active Inbox Runs: %w", err)
	}

	records := make([]inbox.ActiveRun, 0, len(rows))
	for _, row := range rows {
		records = append(records, inbox.ActiveRun{
			ID:          uuid.UUID(row.ID.Bytes),
			TargetName:  row.TargetName,
			ActionName:  row.ActionName,
			ClusterName: row.ClusterName,
			CreatedAt:   row.CreatedAt.Time.UTC(),
			Status: run.ObservationStatus{
				Phase:          run.Phase(row.Phase),
				AssignedAt:     optionalTime(row.AssignedAt),
				LastObservedAt: optionalTime(row.LastObservedAt),
			},
		})
	}

	return records, nil
}

func (d *Database) ListInboxPromotions(ctx context.Context) ([]inbox.PromotionState, error) {
	rows, err := d.queries.ListInboxPromotions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Inbox Promotions: %w", err)
	}

	records := make([]inbox.PromotionState, 0, len(rows))
	for _, row := range rows {
		records = append(records, inbox.PromotionState{
			ID:              uuid.UUID(row.ID.Bytes),
			StageSnapshot:   row.StageSnapshot,
			CandidateBranch: stringValue(row.CandidateBranch),
			TargetBranch:    row.TargetBranch,
			Status:          promotion.PromotionStatus(row.Status),
			Message:         row.Message,
			CreatedAt:       row.CreatedAt.Time.UTC(),
			UpdatedAt:       row.UpdatedAt.Time.UTC(),
		})
	}

	return records, nil
}

func (d *Database) ListInboxFailedGitTriggerDeliveries(ctx context.Context) ([]gittrigger.FailedDelivery, error) {
	rows, err := d.queries.ListInboxFailedGitTriggerDeliveries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list failed Git-trigger deliveries for Inbox: %w", err)
	}

	records := make([]gittrigger.FailedDelivery, 0, len(rows))
	for _, row := range rows {
		diagnostic := ""
		if row.Diagnostic != nil {
			diagnostic = *row.Diagnostic
		}
		records = append(records, gittrigger.FailedDelivery{
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

	return records, nil
}
