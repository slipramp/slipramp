package catalog

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/placement"
)

var ErrNotFound = errors.New("active catalog not found")

type Store interface {
	GetActiveCatalog(ctx context.Context, repositoryID uuid.UUID) (ActiveCatalog, error)
	GetActiveCatalogRevision(ctx context.Context, repositoryID uuid.UUID) (Revision, error)
	GetCatalogRevision(context.Context, uuid.UUID, uuid.UUID) (Revision, error)
	ListActiveTargets(context.Context, TargetListFilter) (TargetList, error)
}

func (s *Service) GetRevision(ctx context.Context, repositoryID uuid.UUID, revisionID uuid.UUID) (Revision, error) {
	return s.catalogs.GetCatalogRevision(ctx, repositoryID, revisionID)
}

type Service struct {
	catalogs   Store
	placements placement.Set
}

func NewService(catalogs Store, placements placement.Set) *Service {
	return &Service{catalogs: catalogs, placements: placements}
}

func (s *Service) GetActiveRevision(ctx context.Context, repositoryID uuid.UUID) (Revision, error) {
	return s.catalogs.GetActiveCatalogRevision(ctx, repositoryID)
}

func (s *Service) GetActive(ctx context.Context, repositoryID uuid.UUID) (ActiveCatalog, error) {
	return s.catalogs.GetActiveCatalog(ctx, repositoryID)
}

func (s *Service) ListTargets(ctx context.Context, filter TargetListFilter) (TargetList, error) {
	return s.catalogs.ListActiveTargets(ctx, filter)
}

func (s *Service) ExplainActiveAction(
	ctx context.Context,
	repositoryID uuid.UUID,
	targetName string,
	actionName string,
) (Explanation, error) {
	revision, err := s.catalogs.GetActiveCatalogRevision(ctx, repositoryID)
	if err != nil {
		return Explanation{}, err
	}

	placements := s.placements
	diagnostics := ValidateDeployment(revision.Catalog, placements)
	result := Result{
		Catalog:     revision.Catalog,
		Diagnostics: diagnostics,
		Level:       ValidationLevelDeployment,
		placements:  &placements,
	}

	return result.ExplainWithOptions(
		targetName,
		actionName,
		nil,
		ExplanationOptions{AllowOmittedRequiredParameters: true},
	)
}
