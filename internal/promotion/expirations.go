package promotion

import (
	"context"
	"time"
)

const DefaultExpirationInterval = time.Minute

type ExpirationStore interface {
	ExpirePendingPromotions(context.Context, time.Time) ([]Promotion, error)
}

type ExpirationReconciler struct {
	store ExpirationStore
	now   func() time.Time
}

func NewExpirationReconciler(store ExpirationStore) *ExpirationReconciler {
	return &ExpirationReconciler{
		store: store,
		now:   time.Now,
	}
}

func (c *ExpirationReconciler) Expire(ctx context.Context) (int, error) {
	records, err := c.store.ExpirePendingPromotions(
		ctx,
		c.now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		return 0, err
	}

	return len(records), nil
}
