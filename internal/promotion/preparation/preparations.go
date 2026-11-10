package preparation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/argodiff"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

const (
	DefaultPreparationInterval = time.Second
)

type PreparationStore interface {
	GetPromotionAwaitingDispatch(context.Context, time.Time) (promotion.Promotion, error)
	RecordPromotionPreparationFailure(
		context.Context,
		promotion.PreparationFailure,
	) (promotion.PreparationFailureState, bool, error)
	ReservePromotionPreparationRevision(
		ctx context.Context,
		promotionID uuid.UUID,
		repositoryID uuid.UUID,
		catalogRevisionID uuid.UUID,
		updatedAt time.Time,
	) (promotion.Promotion, error)
	GetPromotionWithFinishedPreparation(context.Context, time.Time) (promotion.Promotion, error)
	CompletePromotionPreparation(
		ctx context.Context,
		promotionID uuid.UUID,
		runID uuid.UUID,
		gitArtifactID uuid.UUID,
		argoArtifactID uuid.UUID,
		completedAt time.Time,
		notificationPlan *notification.EnqueuePlan,
	) (promotion.Promotion, error)
	FailPromotionPreparation(
		ctx context.Context,
		promotionID uuid.UUID,
		runID uuid.UUID,
		message string,
		completedAt time.Time,
	) (promotion.Promotion, error)
}

type RepositoryLister interface {
	ListRepositories(context.Context) ([]repository.Repository, error)
}

type PreparationCatalogs interface {
	GetActiveRevision(context.Context, uuid.UUID) (catalog.Revision, error)
	GetActiveTargetByName(context.Context, uuid.UUID, string) (catalog.TargetSummary, error)
}

type PreparationRuns interface {
	CreateRun(context.Context, run.CreateRequest) (run.Run, error)
	GetRun(context.Context, uuid.UUID) (run.Run, error)
}

type PreparationArtifacts interface {
	List(context.Context, uuid.UUID) ([]artifact.Record, error)
	OpenArtifact(context.Context, uuid.UUID) (artifact.Content, error)
}

type PreparationNotifications interface {
	Plan(notification.Event) (*notification.EnqueuePlan, error)
}

type PreparationCoordinator struct {
	integrations  promotion.IntegrationSet
	repositories  RepositoryLister
	catalogs      PreparationCatalogs
	runs          PreparationRuns
	artifacts     PreparationArtifacts
	notifications PreparationNotifications
	store         PreparationStore
	interval      time.Duration
	logger        *slog.Logger
	now           func() time.Time
}

func NewPreparationCoordinator(
	integrations promotion.IntegrationSet,
	repositories RepositoryLister,
	catalogs PreparationCatalogs,
	runs PreparationRuns,
	artifacts PreparationArtifacts,
	notifications PreparationNotifications,
	store PreparationStore,
	interval time.Duration,
	logger *slog.Logger,
) *PreparationCoordinator {
	return &PreparationCoordinator{
		integrations:  integrations,
		repositories:  repositories,
		catalogs:      catalogs,
		runs:          runs,
		artifacts:     artifacts,
		notifications: notifications,
		store:         store,
		interval:      interval,
		logger:        logger.With("component", "promotion_preparation"),
		now:           time.Now,
	}
}

func (c *PreparationCoordinator) Run(ctx context.Context) {
	c.processAndLog(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.processAndLog(ctx)
		}
	}
}

func (c *PreparationCoordinator) processAndLog(ctx context.Context) {
	for ctx.Err() == nil {
		processed, err := c.DispatchNext(ctx)
		if err != nil {
			if ctx.Err() == nil {
				c.logger.Warn("dispatch Promotion preparation", "error", err)
			}
			break
		}
		if !processed {
			break
		}
	}

	for ctx.Err() == nil {
		processed, err := c.ReconcileNext(ctx)
		if err != nil {
			if ctx.Err() == nil {
				c.logger.Warn("reconcile Promotion preparation", "error", err)
			}
			break
		}
		if !processed {
			break
		}
	}
}

func (c *PreparationCoordinator) DispatchNext(ctx context.Context) (bool, error) {
	record, err := c.store.GetPromotionAwaitingDispatch(ctx, c.now().UTC())
	if errors.Is(err, promotion.ErrNoPreparationWork) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := c.dispatch(ctx, record); err != nil {
		return c.recordFailure(ctx, record, err)
	}

	return true, nil
}

func (c *PreparationCoordinator) dispatch(ctx context.Context, record promotion.Promotion) error {
	var err error

	if record.Preparation.CatalogRevisionID == nil {
		record, err = c.reservePreparationRevision(ctx, record)
		if err != nil {
			return err
		}
	}
	if record.Preparation.RepositoryID == nil || record.Preparation.CatalogRevisionID == nil {
		return fmt.Errorf("%w: preparation catalog revision was not reserved", promotion.ErrInvalid)
	}

	parameters, err := record.PreparationParameters()
	if err != nil {
		return err
	}
	integration, ok := c.integrations.Lookup(record.Integration)
	if !ok {
		return promotion.ErrIntegrationNotFound
	}
	idempotencyKey := record.ID.String()
	revisionID := *record.Preparation.CatalogRevisionID
	actor := productidentity.Principal{
		ID:          record.RequestedBySnapshot.PrincipalID,
		Kind:        record.RequestedBySnapshot.Kind,
		DisplayName: record.RequestedBySnapshot.DisplayName,
	}
	target, err := c.catalogs.GetActiveTargetByName(
		ctx,
		*record.Preparation.RepositoryID,
		integration.Preparation.Target,
	)
	if errors.Is(err, catalog.ErrTargetNotFound) {
		return run.ErrTargetNotFound
	}
	if err != nil {
		return fmt.Errorf("get Promotion preparation Target: %w", err)
	}
	created, err := c.runs.CreateRun(ctx, run.CreateRequest{
		TargetID:          target.ID,
		ActionName:        integration.Preparation.Action,
		Parameters:        parameters,
		Inputs:            map[string]uuid.UUID{},
		CatalogRevisionID: &revisionID,
		Source: run.Source{
			Type: run.SourceTypePromotion,
			Promotion: &run.PromotionRunSource{
				PromotionID: record.ID,
			},
		},
		IdempotencyKey: &idempotencyKey,
		Actor:          &actor,
	})
	if err != nil {
		return fmt.Errorf("create preparation Run for Promotion %s: %w", record.ID, err)
	}

	c.logger.Info(
		"Promotion preparation Run created",
		"promotion", record.ID,
		"run", created.ID,
		"repository", *record.Preparation.RepositoryID,
		"catalog_revision", revisionID,
	)

	return nil
}

func (c *PreparationCoordinator) reservePreparationRevision(
	ctx context.Context,
	record promotion.Promotion,
) (promotion.Promotion, error) {
	integration, ok := c.integrations.Lookup(record.Integration)
	if !ok {
		return promotion.Promotion{}, promotion.ErrIntegrationNotFound
	}
	repositories, err := c.repositories.ListRepositories(ctx)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("list preparation repositories: %w", err)
	}

	var selected *repository.Repository
	for index := range repositories {
		if repositories[index].Name == integration.Preparation.Repository {
			selected = &repositories[index]
			break
		}
	}
	if selected == nil {
		return promotion.Promotion{}, promotion.ErrRepositoryUnavailable
	}
	revision, err := c.catalogs.GetActiveRevision(ctx, selected.ID)
	if errors.Is(err, catalog.ErrNotFound) {
		return promotion.Promotion{}, fmt.Errorf(
			"%w: preparation catalog is unavailable",
			promotion.ErrRepositoryUnavailable,
		)
	}
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf("get active preparation catalog: %w", err)
	}

	reserved, err := c.store.ReservePromotionPreparationRevision(
		ctx,
		record.ID,
		selected.ID,
		revision.ID,
		c.now().UTC(),
	)
	if err != nil {
		return promotion.Promotion{}, fmt.Errorf(
			"reserve preparation revision for Promotion %s: %w",
			record.ID,
			err,
		)
	}

	return reserved, nil
}

func (c *PreparationCoordinator) ReconcileNext(ctx context.Context) (bool, error) {
	record, err := c.store.GetPromotionWithFinishedPreparation(ctx, c.now().UTC())
	if errors.Is(err, promotion.ErrNoPreparationWork) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := c.reconcile(ctx, record); err != nil {
		return c.recordFailure(ctx, record, err)
	}

	return true, nil
}

func (c *PreparationCoordinator) reconcile(ctx context.Context, record promotion.Promotion) error {
	if record.Preparation.RunID == nil {
		return fmt.Errorf("%w: finished preparation has no Run", promotion.ErrInvalid)
	}

	preparationRun, err := c.runs.GetRun(ctx, *record.Preparation.RunID)
	if err != nil {
		return fmt.Errorf("get preparation Run %s: %w", *record.Preparation.RunID, err)
	}
	completedAt := c.now().UTC()
	if preparationRun.Phase != run.PhaseFinished || preparationRun.Outcome == nil {
		return fmt.Errorf("%w: selected preparation Run is not finished", promotion.ErrTransitionConflict)
	}
	if *preparationRun.Outcome != run.OutcomeSucceeded {
		message := fmt.Sprintf("Preparation Run finished with outcome %s.", *preparationRun.Outcome)
		if _, err := c.store.FailPromotionPreparation(
			ctx,
			record.ID,
			preparationRun.ID,
			message,
			completedAt,
		); err != nil {
			return err
		}

		return nil
	}

	artifacts, err := c.artifacts.List(ctx, preparationRun.ID)
	if err != nil {
		return fmt.Errorf("list preparation Run artifacts: %w", err)
	}
	gitDiff, err := c.validPreparationArtifact(
		ctx,
		artifacts,
		GitDiffArtifactName,
		GitDiffArtifactMediaType,
	)
	if err != nil {
		return err
	}
	argoDiff, err := c.validPreparationArtifact(
		ctx,
		artifacts,
		ArgoDiffArtifactName,
		argodiff.MediaType,
	)
	if err != nil {
		return err
	}

	event, err := c.reviewRequestedEvent(record, completedAt)
	if err != nil {
		return err
	}
	var notificationPlan *notification.EnqueuePlan
	if c.notifications != nil {
		notificationPlan, err = c.notifications.Plan(event)
		if err != nil {
			return fmt.Errorf("plan review-requested notification: %w", err)
		}
	}

	if _, err := c.store.CompletePromotionPreparation(
		ctx,
		record.ID,
		preparationRun.ID,
		gitDiff.ID,
		argoDiff.ID,
		completedAt,
		notificationPlan,
	); err != nil {
		return err
	}
	c.logger.Info(
		"Promotion preparation completed",
		"promotion", record.ID,
		"run", preparationRun.ID,
		"git_diff_artifact", gitDiff.ID,
		"argo_diff_artifact", argoDiff.ID,
	)

	return nil
}

func (c *PreparationCoordinator) recordFailure(
	ctx context.Context,
	record promotion.Promotion,
	cause error,
) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	failure := preparationFailure(record, c.now().UTC(), cause)
	state, recorded, err := c.store.RecordPromotionPreparationFailure(ctx, failure)
	if err != nil {
		return false, fmt.Errorf("record Promotion preparation failure: %w", err)
	}
	if recorded {
		c.logger.Warn(
			"Promotion preparation could not complete",
			"promotion", record.ID,
			"failure_count", state.Count,
			"terminal", state.Terminal,
			"diagnostic", failure.Diagnostic,
		)
	}

	return true, nil
}

func (c *PreparationCoordinator) reviewRequestedEvent(
	record promotion.Promotion,
	occurredAt time.Time,
) (notification.Event, error) {
	stage := record.StageSnapshot
	if stage.Project == "" || stage.Name == "" || record.ReviewContext == nil {
		return notification.Event{}, fmt.Errorf(
			"%w: Promotion Stage notification snapshot is incomplete",
			promotion.ErrInvalid,
		)
	}

	facts := []notification.Fact{{Label: "Repository", Value: record.ReviewContext.RepositorySnapshot.Name}}
	if record.Lifecycle.ExpiresAt != nil {
		facts = append(facts, notification.Fact{
			Label: "Expires",
			Value: record.Lifecycle.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}

	event, err := notification.NewReviewRequestedEvent(notification.ReviewRequestedInput{
		ID:               uuid.New(),
		SubjectType:      notification.SubjectTypePromotion,
		SubjectID:        record.ID,
		OccurredAt:       occurredAt,
		DeduplicationKey: "promotion:" + record.ID.String() + ":review-requested",
		Title:            "Promotion needs review",
		Summary:          stage.Project + "/" + stage.Name + " is ready for review",
		Facts:            facts,
		Action: notification.Action{
			Label: "Review in Slipramp",
			Path:  "/promotions/" + record.ID.String(),
		},
		ExpiresAt: record.Lifecycle.ExpiresAt,
		CreatedAt: occurredAt,
	})
	if err != nil {
		return notification.Event{}, fmt.Errorf("build review-requested notification: %w", err)
	}

	return event, nil
}
