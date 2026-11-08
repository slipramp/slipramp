package controller

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	controllerkargo "github.com/slipramp/slipramp/internal/controller/kargo"
)

const kargoStageObservationPeriod = 30 * time.Second

type kargoStageSnapshotReporter interface {
	ReplaceKargoStageSnapshot(context.Context, string, api.KargoStageSnapshot) error
}

type kargoStageSnapshotter interface {
	Snapshot(context.Context) (api.KargoStageSnapshot, error)
}

type kargoStageObserver struct {
	integration string
	snapshotter kargoStageSnapshotter
	reporter    kargoStageSnapshotReporter
	logger      *slog.Logger
	interval    time.Duration
}

func newKargoStageObservers(
	snapshotters []*controllerkargo.StageSnapshotter,
	reporter kargoStageSnapshotReporter,
	logger *slog.Logger,
) []*kargoStageObserver {
	observers := make([]*kargoStageObserver, 0, len(snapshotters))
	for _, snapshotter := range snapshotters {
		integration := snapshotter.Integration()
		observers = append(observers, &kargoStageObserver{
			integration: integration,
			snapshotter: snapshotter,
			reporter:    reporter,
			logger: logger.With(
				"component", "kargo-stage-observer",
				"integration", integration,
			),
			interval: kargoStageObservationPeriod,
		})
	}

	return observers
}

func (o *kargoStageObserver) Run(ctx context.Context) error {
	var pending *api.KargoStageSnapshot
	for {
		if pending == nil {
			snapshot, err := o.snapshotter.Snapshot(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				o.logger.Warn("observe Kargo Stages", "error", err)
				if !waitForRetry(ctx, o.interval) {
					return nil
				}
				continue
			}
			pending = &snapshot
		}

		err := o.reporter.ReplaceKargoStageSnapshot(ctx, o.integration, *pending)
		if err == nil {
			pending = nil
		} else if ctx.Err() != nil {
			return nil
		} else if errors.Is(err, errControllerCredentialRejected) {
			return err
		} else {
			o.logger.Warn("submit Kargo Stage snapshot", "snapshot_id", pending.SnapshotID, "error", err)
			if permanentServerError(err) {
				pending = nil
			}
		}

		if !waitForRetry(ctx, o.interval) {
			return nil
		}
	}
}
