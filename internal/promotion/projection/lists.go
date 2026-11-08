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

func compareText(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}

	return 0
}

func validateRevision(label, revision string) error {
	if len(revision) != 40 && len(revision) != 64 {
		return fmt.Errorf("%w: %s must be a full Git object ID", promotion.ErrInvalid, label)
	}
	for _, character := range revision {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return fmt.Errorf(
					"%w: %s must be a lowercase full Git object ID",
					promotion.ErrInvalid,
					label,
				)
			}
		}
	}

	return nil
}
