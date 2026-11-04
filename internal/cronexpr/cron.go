// Package cronexpr owns the supported scheduled Action cron grammar.
package cronexpr

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Schedule is a validated five-field cron expression. Callers pass UTC times
// to Next so evaluation never inherits the Server's local timezone.
type Schedule interface {
	Next(time.Time) time.Time
}

func Parse(expression string) (Schedule, error) {
	if len(strings.Fields(expression)) != 5 {
		return nil, fmt.Errorf("invalid five-field cron expression: expected exactly five fields")
	}
	schedule, err := parser.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid five-field cron expression: %w", err)
	}

	return schedule, nil
}
