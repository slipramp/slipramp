package store

import (
	"context"
	"encoding/json"
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
		cancellationState, err := (run.CancellationStatus{
			Phase:                   run.Phase(row.Phase),
			Outcome:                 runOutcome(row.Outcome),
			OutcomeReason:           row.OutcomeReason,
			CreatedAt:               row.CreatedAt.Time.UTC(),
			FinishedAt:              applicationTime(row.FinishedAt),
			CancellationRequestedAt: applicationTime(row.CancellationRequestedAt),
			CanceledByPresent:       row.CanceledByPrincipalID.Valid,
		}).CancellationState()
		if err != nil {
			return nil, fmt.Errorf("derive Inbox Run cancellation state: %w", err)
		}
		records = append(records, inbox.ActiveRun{
			ID:           uuid.UUID(row.ID.Bytes),
			TargetName:   row.TargetName,
			ActionName:   row.ActionName,
			ClusterName:  row.ClusterName,
			CreatedAt:    row.CreatedAt.Time.UTC(),
			Review:       run.ReviewState(row.ReviewState),
			Cancellation: cancellationState,
			Status: run.ObservationStatus{
				Phase:          run.Phase(row.Phase),
				AssignedAt:     applicationTime(row.AssignedAt),
				LastObservedAt: applicationTime(row.LastObservedAt),
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
		var stage struct {
			Name       string `json:"name"`
			Enrichment *struct {
				Application string `json:"application"`
			} `json:"enrichment,omitempty"`
		}
		if err := json.Unmarshal(row.StageSnapshot, &stage); err != nil {
			return nil, fmt.Errorf("decode Inbox Promotion Stage facts: %w", err)
		}
		application := ""
		if stage.Enrichment != nil {
			application = stage.Enrichment.Application
		}
		record := inbox.PromotionState{
			ID:              uuid.UUID(row.ID.Bytes),
			StageName:       stage.Name,
			Application:     application,
			CandidateBranch: textValue(row.CandidateBranch),
			TargetBranch:    textValue(row.TargetBranch),
			Status:          promotion.LifecycleState(row.Status),
			Message:         row.Message,
			CreationDispatch: promotion.CreationDispatchState{
				DispatchedAt: applicationTime(row.DispatchedAt),
				FailedAt:     applicationTime(row.DispatchFailedAt),
				Diagnostic:   row.DispatchDiagnostic,
			},
			CreatedAt: row.CreatedAt.Time.UTC(),
			UpdatedAt: row.UpdatedAt.Time.UTC(),
		}
		if row.KargoOutcomePhase != nil {
			if !row.KargoOutcomeObservedAt.Valid {
				return nil, fmt.Errorf("decode Inbox Promotion Kargo outcome: observation time is missing")
			}
			record.KargoOutcome = &promotion.KargoOutcome{
				Phase:      promotion.KargoPhase(*row.KargoOutcomePhase),
				ObservedAt: row.KargoOutcomeObservedAt.Time.UTC(),
				Diagnostic: row.KargoOutcomeDiagnostic,
			}
		}
		records = append(records, record)
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
		lifecycle := gitTriggerLifecycle(
			row.CreatedAt,
			row.FailureCount,
			row.NextAttemptAt,
			row.Diagnostic,
			row.FailedAt,
			row.RunID,
			row.SupersededAt,
		)
		state, err := lifecycle.State(lifecycle.NextAttemptAt)
		if err != nil {
			return nil, fmt.Errorf(
				"map failed Git-trigger delivery %s for Inbox: %w",
				uuid.UUID(row.ID.Bytes),
				err,
			)
		}
		if state != gittrigger.StateFailed {
			return nil, fmt.Errorf(
				"map failed Git-trigger delivery %s for Inbox: state is %s",
				uuid.UUID(row.ID.Bytes),
				state,
			)
		}
		records = append(records, gittrigger.FailedDelivery{
			ID:                uuid.UUID(row.ID.Bytes),
			TargetID:          uuid.UUID(row.TargetID.Bytes),
			RepositoryID:      uuid.UUID(row.RepositoryID.Bytes),
			RepositoryName:    row.RepositoryName,
			CatalogRevisionID: uuid.UUID(row.CatalogRevisionID.Bytes),
			CommitSHA:         row.CommitSha,
			TargetName:        row.TargetName,
			ActionName:        row.ActionName,
			TriggerName:       row.TriggerName,
			FailureCount:      lifecycle.FailureCount,
			Diagnostic:        lifecycle.Diagnostic,
			FailedAt:          *lifecycle.FailedAt,
		})
	}

	return records, nil
}
