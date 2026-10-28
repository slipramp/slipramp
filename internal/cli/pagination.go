package cli

import (
	"errors"
	"fmt"
	"math"
)

type listPage[T any] struct {
	items  []T
	offset int
	total  int64
}

type invalidListPageError struct {
	message string
}

func (e invalidListPageError) Error() string {
	return e.message
}

func invalidListPage(format string, arguments ...any) error {
	return invalidListPageError{message: fmt.Sprintf(format, arguments...)}
}

func isInvalidListPage(err error) bool {
	var invalid invalidListPageError

	return errors.As(err, &invalid)
}

func collectListPages[T any](
	initialOffset int,
	fetch func(int) (listPage[T], error),
) ([]T, int64, error) {
	items := []T{}
	offset := initialOffset
	for {
		page, err := fetch(offset)
		if err != nil {
			return nil, 0, err
		}
		if page.offset != offset {
			return nil, 0, invalidListPage("requested offset %d but received offset %d", offset, page.offset)
		}
		items = append(items, page.items...)
		if len(page.items) == 0 && int64(offset) >= page.total {
			return items, page.total, nil
		}
		nextOffset := int64(offset) + int64(len(page.items))
		if nextOffset > page.total {
			return nil, 0, invalidListPage(
				"page ending at offset %d exceeds reported total %d",
				nextOffset,
				page.total,
			)
		}
		if nextOffset == page.total {
			return items, page.total, nil
		}
		if len(page.items) == 0 {
			return nil, 0, invalidListPage(
				"empty page at offset %d precedes reported total %d",
				offset,
				page.total,
			)
		}
		if nextOffset > math.MaxInt32 {
			return nil, 0, invalidListPage("next offset %d exceeds the supported maximum", nextOffset)
		}
		offset = int(nextOffset)
	}
}
