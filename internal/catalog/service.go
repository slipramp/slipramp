package catalog

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("active catalog not found")

type Store interface {
	GetActiveCatalogRevision(
		ctx context.Context,
		repositoryID uuid.UUID,
	) (Revision, error)
	GetCatalogRevision(context.Context, uuid.UUID, uuid.UUID) (Revision, error)
}

func (s *Service) GetRevision(
	ctx context.Context,
	repositoryID uuid.UUID,
	revisionID uuid.UUID,
) (Revision, error) {
	return s.catalogs.GetCatalogRevision(ctx, repositoryID, revisionID)
}

type Service struct {
	catalogs Store
}

func NewService(catalogs Store) *Service {
	return &Service{catalogs: catalogs}
}

func (s *Service) GetActiveRevision(
	ctx context.Context,
	repositoryID uuid.UUID,
) (Revision, error) {
	return s.catalogs.GetActiveCatalogRevision(ctx, repositoryID)
}
