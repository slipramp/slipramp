package catalog

import (
	"context"
	"errors"

	"github.com/google/uuid"
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
	catalogs Store
}

func NewService(catalogs Store) *Service {
	return &Service{catalogs: catalogs}
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
