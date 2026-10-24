package run

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCreateRunReturnsIdempotentRunBeforeCatalogResolution(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	idempotencyKey := "manual-network-preview"
	request := CreateRequest{
		RepositoryID:   testRepositoryID,
		TargetName:     "network",
		ActionName:     "preview",
		Parameters:     map[string]any{"mode": "safe"},
		Source:         Source{Type: SourceTypeManual},
		IdempotencyKey: &idempotencyKey,
	}
	digest, err := requestDigest(request, request.Parameters, nil)
	if err != nil {
		t.Fatalf("requestDigest() error = %v", err)
	}
	want := Run{
		ID:            uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1209"),
		RequestDigest: digest,
	}
	runs.existing = want
	runs.getError = nil
	service.repositories = &recordingRepositories{err: errors.New("must not be called")}
	service.catalogs = &recordingCatalogs{err: errors.New("must not be called")}

	created, err := service.CreateRun(context.Background(), request)
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.ID != want.ID {
		t.Errorf("created ID = %s, want %s", created.ID, want.ID)
	}
	if runs.calls != 0 {
		t.Errorf("creation calls = %d, want 0", runs.calls)
	}
}
