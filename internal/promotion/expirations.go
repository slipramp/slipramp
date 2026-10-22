package promotion

import (
	"context"
	"log/slog"
	"time"
)

const DefaultExpirationInterval = time.Minute

type ExpirationStore interface {
	ExpirePendingPromotions(context.Context, time.Time) ([]Promotion, error)
}

type ExpirationCoordinator struct {
	store    ExpirationStore
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time
}

func NewExpirationCoordinator(
	store ExpirationStore,
	interval time.Duration,
	logger *slog.Logger,
) *ExpirationCoordinator {
	return &ExpirationCoordinator{
		store:    store,
		interval: interval,
		logger:   logger.With("component", "promotion_expiration"),
		now:      time.Now,
	}
}

func (c *ExpirationCoordinator) Run(ctx context.Context) {
	c.expireAndLog(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.expireAndLog(ctx)
		}
	}
}

func (c *ExpirationCoordinator) Expire(ctx context.Context) (int, error) {
	records, err := c.store.ExpirePendingPromotions(
		ctx,
		c.now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		return 0, err
	}

	return len(records), nil
}

func (c *ExpirationCoordinator) expireAndLog(ctx context.Context) {
	count, err := c.Expire(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Error("expire pending Promotions", "error", err)
		}

		return
	}
	if count > 0 {
		c.logger.Info("expired pending Promotions", "count", count)
	}
}
