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
