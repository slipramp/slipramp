package gate

import (
	"context"
	"log/slog"
	"time"
)

type WindowStore interface {
	WindowCheckTime(context.Context) (time.Time, error)
	RefreshServerWindows(context.Context, time.Time) (time.Duration, error)
}

type WindowCoordinator struct {
	store  WindowStore
	logger *slog.Logger
	wake   chan struct{}
}

func NewWindowCoordinator(store WindowStore, logger *slog.Logger) *WindowCoordinator {
	return &WindowCoordinator{store: store, logger: logger.With("component", "gate_windows"), wake: make(chan struct{}, 1)}
}

func (c *WindowCoordinator) Notify() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *WindowCoordinator) Run(ctx context.Context) {
	var startedAt time.Time
	for ctx.Err() == nil {
		var err error
		if startedAt.IsZero() {
			startedAt, err = c.store.WindowCheckTime(ctx)
		}
		delay := time.Second
		if err == nil {
			delay, err = c.store.RefreshServerWindows(ctx, startedAt)
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.logger.WarnContext(ctx, "refresh Gate windows", "error", err)
			delay = time.Second
		}
		// Rediscover writes from other Server processes even without a local wakeup.
		timer := time.NewTimer(min(delay, time.Minute))
		select {
		case <-ctx.Done():
		case <-c.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}
