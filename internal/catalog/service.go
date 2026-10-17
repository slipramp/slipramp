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
}

type revisionStore interface {
	GetCatalogRevision(context.Context, uuid.UUID, uuid.UUID) (Revision, error)
}

func (s *Service) GetRevision(
	ctx context.Context,
	repositoryID uuid.UUID,
	revisionID uuid.UUID,
) (Revision, error) {
	store, ok := s.catalogs.(revisionStore)
	if !ok {
		return Revision{}, errors.New("catalog store cannot load immutable revisions")
	}

	return store.GetCatalogRevision(ctx, repositoryID, revisionID)
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
