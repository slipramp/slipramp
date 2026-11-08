package controller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

type kargoStageSnapshotterFunc func(context.Context) (api.KargoStageSnapshot, error)

func (f kargoStageSnapshotterFunc) Snapshot(ctx context.Context) (api.KargoStageSnapshot, error) {
	return f(ctx)
}

type kargoStageSnapshotReporterFunc func(context.Context, string, api.KargoStageSnapshot) error

func (f kargoStageSnapshotReporterFunc) ReplaceKargoStageSnapshot(
	ctx context.Context,
	integration string,
	snapshot api.KargoStageSnapshot,
) error {
	return f(ctx, integration, snapshot)
}

func TestKargoStageObserverRetriesTheSameSnapshotAfterAmbiguousFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	want := api.KargoStageSnapshot{
		SnapshotID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1904"),
		ObservedAt: time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC),
		Stages:     []api.PromotionStageObservation{},
	}
	observations := 0
	var reported []api.KargoStageSnapshot
	observer := &kargoStageObserver{
		integration: "prod",
		snapshotter: kargoStageSnapshotterFunc(func(context.Context) (api.KargoStageSnapshot, error) {
			observations++
			return want, nil
		}),
		reporter: kargoStageSnapshotReporterFunc(func(
			_ context.Context,
			integration string,
			snapshot api.KargoStageSnapshot,
		) error {
			if integration != "prod" {
				t.Errorf("integration = %q, want prod", integration)
			}
			reported = append(reported, snapshot)
			if len(reported) == 1 {
				return errors.New("ambiguous network failure")
			}
			cancel()
			return nil
		}),
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		interval: time.Millisecond,
	}

	if err := observer.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if observations != 1 || len(reported) != 2 || !reflect.DeepEqual(reported[0], reported[1]) {
		t.Fatalf("observations = %d, reported snapshots = %#v", observations, reported)
	}
}
