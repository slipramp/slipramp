package handler

import "fmt"

const (
	defaultListPageSize = 25
	maximumListPageSize = 200
)

func listWindow(limitValue *int, offsetValue *int32) (int, int, error) {
	limit := defaultListPageSize
	if limitValue != nil {
		limit = *limitValue
	}
	if limit < 1 || limit > maximumListPageSize {
		return 0, 0, fmt.Errorf("limit must be between 1 and %d", maximumListPageSize)
	}

	offset := 0
	if offsetValue != nil {
		offset = int(*offsetValue)
	}
	if offset < 0 {
		return 0, 0, fmt.Errorf("offset must not be negative")
	}

	return limit, offset, nil
}

func listPage[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return []T{}
	}

	return items[offset:min(offset+limit, len(items))]
}
