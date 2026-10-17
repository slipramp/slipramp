package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestServiceGetsActiveRevision(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	want := Revision{
		ID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201"),
	}
	store := &recordingCatalogStore{revision: want}
	service := NewService(store)

	got, err := service.GetActiveRevision(context.Background(), repositoryID)
	if err != nil {
		t.Fatalf("GetActiveRevision() error = %v", err)
	}

	if store.repositoryID != repositoryID {
		t.Errorf("repository ID = %s, want %s", store.repositoryID, repositoryID)
	}
	if got.ID != want.ID {
		t.Errorf("revision ID = %s, want %s", got.ID, want.ID)
	}
}

type recordingCatalogStore struct {
	repositoryID uuid.UUID
	revision     Revision
}

func (s *recordingCatalogStore) GetActiveCatalogRevision(
	_ context.Context,
	repositoryID uuid.UUID,
) (Revision, error) {
	s.repositoryID = repositoryID

	return s.revision, nil
}
