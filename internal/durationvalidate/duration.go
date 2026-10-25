// Package durationvalidate parses duration-shaped configuration values with
// the shared positive-duration invariant used by execution configuration.
package durationvalidate

import (
	"fmt"
	"time"
)

func Positive(subject, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", subject, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be positive", subject)
	}

	return duration, nil
}
