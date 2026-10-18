package artifact

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/objectstore"
)

type retentionMetadataStore struct {
	records    map[uuid.UUID]Artifact
	objectKeys map[uuid.UUID][]string
	references map[string]bool
}

func (s *retentionMetadataStore) ExpireArtifacts(
	_ context.Context,
	sealedBefore time.Time,
	limit int32,
) ([]Artifact, error) {
	result := make([]Artifact, 0)
	for id, record := range s.records {
		if int32(len(result)) == limit {
			break
		}
		if record.State != StateSealed || record.RetentionState != RetentionRetained ||
			record.SealedAt == nil || record.SealedAt.After(sealedBefore) {
			continue
		}

		record.RetentionState = RetentionExpired
		s.records[id] = record
		result = append(result, record)
	}

	return result, nil
}

func (s *retentionMetadataStore) ListExpiredArtifacts(
	context.Context,
) ([]Artifact, error) {
	result := make([]Artifact, 0)
	for _, record := range s.records {
		if record.RetentionState == RetentionExpired {
			result = append(result, record)
		}
	}

	return result, nil
}

func (s *retentionMetadataStore) ListArtifactObjectKeys(
	_ context.Context,
	artifactID uuid.UUID,
) ([]string, error) {
	return slices.Clone(s.objectKeys[artifactID]), nil
}

func (s *retentionMetadataStore) MarkArtifactDeleted(
	_ context.Context,
	artifactID uuid.UUID,
) error {
	record, ok := s.records[artifactID]
	if !ok || record.RetentionState != RetentionExpired {
		return ErrNotFound
	}

	record.RetentionState = RetentionDeleted
	s.records[artifactID] = record

	return nil
}

func (s *retentionMetadataStore) ArtifactObjectKeyExists(
	_ context.Context,
	key string,
) (bool, error) {
	return s.references[key], nil
}

type retentionContentStore struct {
	objects     map[string]objectstore.ObjectInfo
	deleteError map[string]error
	deleted     []string
}

func (s *retentionContentStore) Delete(_ context.Context, key string) error {
	if err := s.deleteError[key]; err != nil {
		return err
	}

	delete(s.objects, key)
	s.deleted = append(s.deleted, key)

	return nil
}

func (s *retentionContentStore) List(
	_ context.Context,
	prefix string,
) ([]objectstore.ObjectInfo, error) {
	result := make([]objectstore.ObjectInfo, 0, len(s.objects))
	for _, object := range s.objects {
		if len(object.Key) >= len(prefix) && object.Key[:len(prefix)] == prefix {
			result = append(result, object)
		}
	}

	return result, nil
}

func TestReconcilerExpiresDeletesAndRemovesOldOrphans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	oldSeal := now.Add(-31 * 24 * time.Hour)
	recentSeal := now.Add(-29 * 24 * time.Hour)
	oldArtifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1600")
	recentArtifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1601")
	oldObjectKey := "runs/old/artifacts/plan/content"
	recentObjectKey := "runs/recent/artifacts/plan/content"
	oldOrphanKey := "runs/orphan/artifacts/plan/content"
	recentOrphanKey := "runs/uploading/artifacts/plan/content"
	metadata := &retentionMetadataStore{
		records: map[uuid.UUID]Artifact{
			oldArtifactID: {
				ID: oldArtifactID, State: StateSealed,
				RetentionState: RetentionRetained, SealedAt: &oldSeal,
			},
			recentArtifactID: {
				ID: recentArtifactID, State: StateSealed,
				RetentionState: RetentionRetained, SealedAt: &recentSeal,
			},
		},
		objectKeys: map[uuid.UUID][]string{oldArtifactID: {oldObjectKey}},
		references: map[string]bool{
			recentObjectKey: true,
		},
	}
	contents := &retentionContentStore{objects: map[string]objectstore.ObjectInfo{
		oldObjectKey: {
			Key: oldObjectKey, LastModified: oldSeal,
		},
		recentObjectKey: {
			Key: recentObjectKey, LastModified: recentSeal,
		},
		oldOrphanKey: {
			Key: oldOrphanKey, LastModified: now.Add(-25 * time.Hour),
		},
		recentOrphanKey: {
			Key: recentOrphanKey, LastModified: now.Add(-23 * time.Hour),
		},
	}}
	reconciler := newTestReconciler(metadata, contents)
	reconciler.now = func() time.Time { return now }

	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result != (ReconciliationResult{
		ExpiredArtifacts: 1,
		DeletedArtifacts: 1,
		DeletedOrphans:   1,
	}) {
		t.Errorf("Reconcile() result = %#v", result)
	}
	if metadata.records[oldArtifactID].RetentionState != RetentionDeleted {
		t.Errorf("old Artifact retention = %q, want deleted", metadata.records[oldArtifactID].RetentionState)
	}
	if metadata.records[recentArtifactID].RetentionState != RetentionRetained {
		t.Errorf("recent Artifact retention = %q, want retained", metadata.records[recentArtifactID].RetentionState)
	}
	slices.Sort(contents.deleted)
	wantDeleted := []string{oldObjectKey, oldOrphanKey}
	slices.Sort(wantDeleted)
	if !slices.Equal(contents.deleted, wantDeleted) {
		t.Errorf("deleted objects = %v, want %v", contents.deleted, wantDeleted)
	}
}

func TestReconcilerRetriesExpiredArtifactAfterObjectDeletionFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1602")
	objectKey := "runs/retry/artifacts/log/chunks/0-a"
	metadata := &retentionMetadataStore{
		records: map[uuid.UUID]Artifact{
			artifactID: {
				ID: artifactID, State: StateSealed,
				RetentionState: RetentionExpired,
			},
		},
		objectKeys: map[uuid.UUID][]string{artifactID: {objectKey}},
		references: map[string]bool{objectKey: true},
	}
	contents := &retentionContentStore{
		objects: map[string]objectstore.ObjectInfo{
			objectKey: {Key: objectKey, LastModified: now.Add(-48 * time.Hour)},
		},
		deleteError: map[string]error{objectKey: errors.New("object store unavailable")},
	}
	reconciler := newTestReconciler(metadata, contents)
	reconciler.now = func() time.Time { return now }

	result, err := reconciler.Reconcile(context.Background())
	if err == nil {
		t.Fatal("Reconcile() error = nil, want deletion failure")
	}
	if result.DeletedArtifacts != 0 ||
		metadata.records[artifactID].RetentionState != RetentionExpired {
		t.Errorf("failed deletion result = %#v, Artifact = %#v", result, metadata.records[artifactID])
	}

	delete(contents.deleteError, objectKey)
	result, err = reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("retry Reconcile() error = %v", err)
	}
	if result.DeletedArtifacts != 1 ||
		metadata.records[artifactID].RetentionState != RetentionDeleted {
		t.Errorf("retry result = %#v, Artifact = %#v", result, metadata.records[artifactID])
	}
}

func newTestReconciler(
	metadata RetentionStore,
	contents RetentionObjectStore,
) *Reconciler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewReconciler(
		metadata,
		contents,
		DefaultRetentionPeriod,
		DefaultReconcileInterval,
		DefaultOrphanGracePeriod,
		logger,
	)
}
