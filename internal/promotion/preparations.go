package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

const (
	DefaultPreparationInterval = time.Second
)

type PreparationStore interface {
	GetPromotionAwaitingDispatch(context.Context, time.Time) (Promotion, error)
	RecordPromotionPreparationFailure(context.Context, PreparationFailure) (PreparationFailureState, bool, error)
	ReservePromotionPreparationRevision(
		ctx context.Context,
		promotionID uuid.UUID,
		repositoryID uuid.UUID,
		catalogRevisionID uuid.UUID,
		updatedAt time.Time,
	) (Promotion, error)
	GetPromotionWithFinishedPreparation(context.Context, time.Time) (Promotion, error)
	CompletePromotionPreparation(
		ctx context.Context,
		promotionID uuid.UUID,
		runID uuid.UUID,
		gitArtifactID uuid.UUID,
		argoArtifactID uuid.UUID,
		completedAt time.Time,
		notificationPlan *notification.EnqueuePlan,
	) (Promotion, error)
	FailPromotionPreparation(
		ctx context.Context,
		promotionID uuid.UUID,
		runID uuid.UUID,
		message string,
		completedAt time.Time,
	) (Promotion, error)
}

type PreparationCatalogs interface {
	GetActiveRevision(context.Context, uuid.UUID) (catalog.Revision, error)
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
	integrations  IntegrationSet
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
	integrations IntegrationSet,
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
	if errors.Is(err, ErrNoPreparationWork) {
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

func (c *PreparationCoordinator) dispatch(ctx context.Context, record Promotion) error {
	var err error

	if record.PreparationCatalogRevisionID == nil {
		record, err = c.reservePreparationRevision(ctx, record)
		if err != nil {
			return err
		}
	}
	if record.PreparationRepositoryID == nil || record.PreparationCatalogRevisionID == nil {
		return fmt.Errorf("%w: preparation catalog revision was not reserved", ErrInvalid)
	}

	parameters, err := record.PreparationParameters()
	if err != nil {
		return err
	}
	integration, ok := c.integrations.Lookup(record.Integration)
	if !ok {
		return ErrIntegrationNotFound
	}
	idempotencyKey := record.ID.String()
	revisionID := *record.PreparationCatalogRevisionID
	actor := productidentity.Principal{
		ID:          record.RequestedBySnapshot.PrincipalID,
		Kind:        record.RequestedBySnapshot.Kind,
		DisplayName: record.RequestedBySnapshot.DisplayName,
	}
	created, err := c.runs.CreateRun(ctx, run.CreateRequest{
		RepositoryID:      *record.PreparationRepositoryID,
		TargetName:        integration.Preparation.Target,
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
		"repository", *record.PreparationRepositoryID,
		"catalog_revision", revisionID,
	)

	return nil
}

func (c *PreparationCoordinator) reservePreparationRevision(
	ctx context.Context,
	record Promotion,
) (Promotion, error) {
	integration, ok := c.integrations.Lookup(record.Integration)
	if !ok {
		return Promotion{}, ErrIntegrationNotFound
	}
	repositories, err := c.repositories.ListRepositories(ctx)
	if err != nil {
		return Promotion{}, fmt.Errorf("list preparation repositories: %w", err)
	}

	var selected *repository.Repository
	for index := range repositories {
		if repositories[index].Name == integration.Preparation.Repository {
			selected = &repositories[index]
			break
		}
	}
	if selected == nil {
		return Promotion{}, ErrRepositoryUnavailable
	}
	revision, err := c.catalogs.GetActiveRevision(ctx, selected.ID)
	if errors.Is(err, catalog.ErrNotFound) {
		return Promotion{}, fmt.Errorf("%w: preparation catalog is unavailable", ErrRepositoryUnavailable)
	}
	if err != nil {
		return Promotion{}, fmt.Errorf("get active preparation catalog: %w", err)
	}

	reserved, err := c.store.ReservePromotionPreparationRevision(
		ctx,
		record.ID,
		selected.ID,
		revision.ID,
		c.now().UTC(),
	)
	if err != nil {
		return Promotion{}, fmt.Errorf("reserve preparation revision for Promotion %s: %w", record.ID, err)
	}

	return reserved, nil
}

func (c *PreparationCoordinator) ReconcileNext(ctx context.Context) (bool, error) {
	record, err := c.store.GetPromotionWithFinishedPreparation(ctx, c.now().UTC())
	if errors.Is(err, ErrNoPreparationWork) {
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

func (c *PreparationCoordinator) reconcile(ctx context.Context, record Promotion) error {
	if record.PreparationRunID == nil {
		return fmt.Errorf("%w: finished preparation has no Run", ErrInvalid)
	}

	preparationRun, err := c.runs.GetRun(ctx, *record.PreparationRunID)
	if err != nil {
		return fmt.Errorf("get preparation Run %s: %w", *record.PreparationRunID, err)
	}
	completedAt := c.now().UTC()
	if preparationRun.Phase != run.PhaseFinished || preparationRun.Outcome == nil {
		return fmt.Errorf("%w: selected preparation Run is not finished", ErrTransitionConflict)
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
		ArgoDiffArtifactMediaType,
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

func (c *PreparationCoordinator) recordFailure(ctx context.Context, record Promotion, cause error) (bool, error) {
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

func (c *PreparationCoordinator) reviewRequestedEvent(record Promotion, occurredAt time.Time) (notification.Event, error) {
	var stage PromotionStageSnapshot
	if err := json.Unmarshal(record.StageSnapshot, &stage); err != nil {
		return notification.Event{}, fmt.Errorf("decode Promotion Stage snapshot for notification: %w", err)
	}
	if stage.Project == "" || stage.Name == "" || stage.RepositoryName == "" {
		return notification.Event{}, fmt.Errorf("%w: Promotion Stage notification snapshot is incomplete", ErrInvalid)
	}

	facts := []notification.Fact{{Label: "Repository", Value: stage.RepositoryName}}
	if record.ExpiresAt != nil {
		facts = append(facts, notification.Fact{
			Label: "Expires",
			Value: record.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}

	event, err := notification.NewReviewRequestedEvent(notification.ReviewRequestedInput{
		ID:               uuid.New(),
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
		ExpiresAt: record.ExpiresAt,
		CreatedAt: occurredAt,
	})
	if err != nil {
		return notification.Event{}, fmt.Errorf("build review-requested notification: %w", err)
	}

	return event, nil
}
