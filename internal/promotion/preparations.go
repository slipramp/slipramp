package promotion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

const (
	DefaultPreparationInterval = time.Second
)

type PreparationStore interface {
	GetPromotionAwaitingDispatch(context.Context) (Promotion, error)
	ReservePromotionPreparationRevision(
		ctx context.Context,
		promotionID uuid.UUID,
		repositoryID uuid.UUID,
		catalogRevisionID uuid.UUID,
		updatedAt time.Time,
	) (Promotion, error)
	AttachPromotionPreparationRun(
		ctx context.Context,
		promotionID uuid.UUID,
		runID uuid.UUID,
		updatedAt time.Time,
	) (Promotion, error)
	GetPromotionWithFinishedPreparation(context.Context) (Promotion, error)
	CompletePromotionPreparation(
		ctx context.Context,
		promotionID uuid.UUID,
		runID uuid.UUID,
		gitArtifactID uuid.UUID,
		argoArtifactID uuid.UUID,
		completedAt time.Time,
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

type PreparationCoordinator struct {
	integrations IntegrationSet
	repositories RepositoryLister
	catalogs     PreparationCatalogs
	runs         PreparationRuns
	artifacts    PreparationArtifacts
	store        PreparationStore
	interval     time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

func NewPreparationCoordinator(
	integrations IntegrationSet,
	repositories RepositoryLister,
	catalogs PreparationCatalogs,
	runs PreparationRuns,
	artifacts PreparationArtifacts,
	store PreparationStore,
	interval time.Duration,
	logger *slog.Logger,
) *PreparationCoordinator {
	return &PreparationCoordinator{
		integrations: integrations,
		repositories: repositories,
		catalogs:     catalogs,
		runs:         runs,
		artifacts:    artifacts,
		store:        store,
		interval:     interval,
		logger:       logger.With("component", "promotion_preparation"),
		now:          time.Now,
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
	record, err := c.store.GetPromotionAwaitingDispatch(ctx)
	if errors.Is(err, ErrNoPreparationWork) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if record.PreparationCatalogRevisionID == nil {
		record, err = c.reservePreparationRevision(ctx, record)
		if err != nil {
			return false, err
		}
	}
	if record.PreparationRepositoryID == nil || record.PreparationCatalogRevisionID == nil {
		return false, fmt.Errorf("%w: preparation catalog revision was not reserved", ErrInvalid)
	}

	parameters, err := record.PreparationParameters()
	if err != nil {
		return false, err
	}
	integration, ok := c.integrations.Lookup(record.Integration)
	if !ok {
		return false, ErrIntegrationNotFound
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
		return false, fmt.Errorf("create preparation Run for Promotion %s: %w", record.ID, err)
	}

	if _, err := c.store.AttachPromotionPreparationRun(ctx, record.ID, created.ID, c.now().UTC()); err != nil {
		return false, fmt.Errorf("attach preparation Run to Promotion %s: %w", record.ID, err)
	}
	c.logger.Info(
		"Promotion preparation Run created",
		"promotion", record.ID,
		"run", created.ID,
		"repository", *record.PreparationRepositoryID,
		"catalog_revision", revisionID,
	)

	return true, nil
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
	record, err := c.store.GetPromotionWithFinishedPreparation(ctx)
	if errors.Is(err, ErrNoPreparationWork) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if record.PreparationRunID == nil {
		return false, fmt.Errorf("%w: finished preparation has no Run", ErrInvalid)
	}

	preparationRun, err := c.runs.GetRun(ctx, *record.PreparationRunID)
	if err != nil {
		return false, fmt.Errorf("get preparation Run %s: %w", *record.PreparationRunID, err)
	}
	completedAt := c.now().UTC()
	if preparationRun.Phase != run.PhaseFinished || preparationRun.Outcome == nil {
		return false, fmt.Errorf("%w: selected preparation Run is not finished", ErrTransitionConflict)
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
			return false, err
		}

		return true, nil
	}

	artifacts, err := c.artifacts.List(ctx, preparationRun.ID)
	if err != nil {
		return false, fmt.Errorf("list preparation Run artifacts: %w", err)
	}
	gitDiff, err := c.validPreparationArtifact(
		ctx,
		artifacts,
		GitDiffArtifactName,
		GitDiffArtifactMediaType,
	)
	if err != nil {
		if errors.Is(err, ErrInvalidPreparationArtifact) {
			return c.failInvalidArtifacts(ctx, record, preparationRun.ID, completedAt, err)
		}

		return false, err
	}
	argoDiff, err := c.validPreparationArtifact(
		ctx,
		artifacts,
		ArgoDiffArtifactName,
		ArgoDiffArtifactMediaType,
	)
	if err != nil {
		if errors.Is(err, ErrInvalidPreparationArtifact) {
			return c.failInvalidArtifacts(ctx, record, preparationRun.ID, completedAt, err)
		}

		return false, err
	}

	if _, err := c.store.CompletePromotionPreparation(
		ctx,
		record.ID,
		preparationRun.ID,
		gitDiff.ID,
		argoDiff.ID,
		completedAt,
	); err != nil {
		return false, err
	}
	c.logger.Info(
		"Promotion preparation completed",
		"promotion", record.ID,
		"run", preparationRun.ID,
		"git_diff_artifact", gitDiff.ID,
		"argo_diff_artifact", argoDiff.ID,
	)

	return true, nil
}
