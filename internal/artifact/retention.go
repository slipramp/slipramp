package artifact

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/objectstore"
)

const (
	DefaultRetentionPeriod   = 30 * 24 * time.Hour
	DefaultReconcileInterval = time.Hour
	DefaultOrphanGracePeriod = 24 * time.Hour

	reconciliationBatchSize int32 = 100
	artifactObjectPrefix          = "runs/"
)

type RetentionStore interface {
	ExpireArtifacts(ctx context.Context, sealedBefore time.Time, limit int32) ([]Artifact, error)
	ListExpiredArtifacts(ctx context.Context) ([]Artifact, error)
	ListArtifactObjectKeys(ctx context.Context, artifactID uuid.UUID) ([]string, error)
	MarkArtifactDeleted(ctx context.Context, artifactID uuid.UUID) error
	ArtifactObjectKeyExists(ctx context.Context, key string) (bool, error)
}

type RetentionObjectStore interface {
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]objectstore.ObjectInfo, error)
}

type ReconciliationResult struct {
	ExpiredArtifacts int
	DeletedArtifacts int
	DeletedOrphans   int
}

type Reconciler struct {
	metadata          RetentionStore
	contents          RetentionObjectStore
	retentionPeriod   time.Duration
	period            time.Duration
	orphanGracePeriod time.Duration
	logger            *slog.Logger
	now               func() time.Time
}

func NewReconciler(
	metadata RetentionStore,
	contents RetentionObjectStore,
	retentionPeriod time.Duration,
	period time.Duration,
	orphanGracePeriod time.Duration,
	logger *slog.Logger,
) *Reconciler {
	return &Reconciler{
		metadata:          metadata,
		contents:          contents,
		retentionPeriod:   retentionPeriod,
		period:            period,
		orphanGracePeriod: orphanGracePeriod,
		logger:            logger.With("component", "artifact_reconciler"),
		now:               time.Now,
	}
}

func (r *Reconciler) Run(ctx context.Context) {
	r.reconcileAndLog(ctx)

	ticker := time.NewTicker(r.period)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcileAndLog(ctx)
		}
	}
}

func (r *Reconciler) Reconcile(ctx context.Context) (ReconciliationResult, error) {
	now := r.now().UTC()
	result := ReconciliationResult{}
	var reconciliationErr error

	for {
		expired, err := r.metadata.ExpireArtifacts(ctx, now.Add(-r.retentionPeriod), reconciliationBatchSize)
		if err != nil {
			reconciliationErr = errors.Join(reconciliationErr, fmt.Errorf("expire retained artifacts: %w", err))
			break
		}

		result.ExpiredArtifacts += len(expired)
		if len(expired) < int(reconciliationBatchSize) {
			break
		}
	}

	deleted, err := r.deleteExpiredArtifacts(ctx)
	result.DeletedArtifacts = deleted
	if err != nil {
		reconciliationErr = errors.Join(reconciliationErr, err)
	}

	deleted, err = r.deleteOrphans(ctx, now.Add(-r.orphanGracePeriod))
	result.DeletedOrphans = deleted
	if err != nil {
		reconciliationErr = errors.Join(reconciliationErr, err)
	}

	return result, reconciliationErr
}

func (r *Reconciler) reconcileAndLog(ctx context.Context) {
	result, err := r.Reconcile(ctx)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("reconcile artifact contents", "error", err)
		}

		return
	}

	if result == (ReconciliationResult{}) {
		return
	}

	r.logger.Info(
		"reconciled artifact contents",
		"expired_artifacts", result.ExpiredArtifacts,
		"deleted_artifacts", result.DeletedArtifacts,
		"deleted_orphans", result.DeletedOrphans,
	)
}

func (r *Reconciler) deleteExpiredArtifacts(ctx context.Context) (int, error) {
	records, err := r.metadata.ListExpiredArtifacts(ctx)
	if err != nil {
		return 0, fmt.Errorf("list artifacts pending content deletion: %w", err)
	}

	deleted := 0
	var deletionErr error
	for _, record := range records {
		keys, err := r.metadata.ListArtifactObjectKeys(ctx, record.ID)
		if err != nil {
			deletionErr = errors.Join(deletionErr, fmt.Errorf("list object keys for artifact %s: %w", record.ID, err))
			continue
		}

		contentsDeleted := true
		for _, key := range keys {
			if err := r.contents.Delete(ctx, key); err != nil {
				contentsDeleted = false
				deletionErr = errors.Join(deletionErr, fmt.Errorf("delete artifact object %q: %w", key, err))
			}
		}
		if !contentsDeleted {
			continue
		}

		if err := r.metadata.MarkArtifactDeleted(ctx, record.ID); err != nil {
			deletionErr = errors.Join(deletionErr, fmt.Errorf("finish deleting artifact %s: %w", record.ID, err))
			continue
		}

		deleted++
	}

	return deleted, deletionErr
}

func (r *Reconciler) deleteOrphans(ctx context.Context, modifiedBefore time.Time) (int, error) {
	objects, err := r.contents.List(ctx, artifactObjectPrefix)
	if err != nil {
		return 0, fmt.Errorf("list artifact objects: %w", err)
	}

	deleted := 0
	var deletionErr error
	for _, object := range objects {
		if object.LastModified.After(modifiedBefore) {
			continue
		}

		exists, err := r.metadata.ArtifactObjectKeyExists(ctx, object.Key)
		if err != nil {
			deletionErr = errors.Join(deletionErr, fmt.Errorf("check artifact object %q: %w", object.Key, err))
			continue
		}
		if exists {
			continue
		}

		if err := r.contents.Delete(ctx, object.Key); err != nil {
			deletionErr = errors.Join(
				deletionErr,
				fmt.Errorf("delete orphaned artifact object %q: %w", object.Key, err),
			)
			continue
		}

		deleted++
	}

	return deleted, deletionErr
}
