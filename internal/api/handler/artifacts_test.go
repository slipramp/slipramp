package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/artifact"
)

func TestGetArtifactStorageUsage(t *testing.T) {
	t.Parallel()

	service := &recordingArtifactService{storageUsage: artifact.StorageUsage{
		ArtifactCount: 24,
		RecordedBytes: 5_368_709_120,
	}}
	handler := newHandler(Dependencies{Artifacts: service})

	response, err := handler.GetArtifactStorageUsage(
		context.Background(),
		api.GetArtifactStorageUsageRequestObject{},
	)
	if err != nil {
		t.Fatalf("GetArtifactStorageUsage() error = %v", err)
	}
	result, ok := response.(api.GetArtifactStorageUsage200JSONResponse)
	if !ok {
		t.Fatalf("GetArtifactStorageUsage() response = %T", response)
	}
	if result.ArtifactCount != 24 || result.RecordedBytes != 5_368_709_120 {
		t.Errorf("GetArtifactStorageUsage() result = %#v", result)
	}
}

func TestGetArtifactStorageUsageReturnsFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("database unavailable")
	handler := newHandler(Dependencies{Artifacts: &recordingArtifactService{storageErr: want}})

	response, err := handler.GetArtifactStorageUsage(
		context.Background(),
		api.GetArtifactStorageUsageRequestObject{},
	)
	if response != nil || !errors.Is(err, want) {
		t.Fatalf("GetArtifactStorageUsage() = (%T, %v), want (nil, %v)", response, err, want)
	}
}
