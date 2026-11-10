package projection

import (
	"fmt"

	"github.com/slipramp/slipramp/internal/promotion"
)

func validateListWindow(limit, offset int) error {
	if limit < 1 || limit > promotion.MaximumListLimit {
		return fmt.Errorf(
			"%w: limit must be between 1 and %d",
			promotion.ErrInvalid,
			promotion.MaximumListLimit,
		)
	}
	if offset < 0 {
		return fmt.Errorf("%w: offset must not be negative", promotion.ErrInvalid)
	}

	return nil
}

func page[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := min(offset+limit, len(items))

	return items[offset:end]
}
