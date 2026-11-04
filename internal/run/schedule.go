package run

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/cronexpr"
)

// ScheduleOccurrenceIdempotencyKey returns a bounded identity for one named
// schedule occurrence without placing catalog-controlled names in a database key.
func ScheduleOccurrenceIdempotencyKey(
	targetID uuid.UUID,
	actionName string,
	triggerName string,
	scheduledAt time.Time,
) string {
	parts := []string{
		targetID.String(),
		actionName,
		triggerName,
		scheduledAt.UTC().Format(time.RFC3339),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return hex.EncodeToString(digest[:])
}

func validateScheduledTriggerRequest(
	action catalog.EffectiveAction,
	source Source,
	parameters map[string]any,
) error {
	if source.Type != SourceTypeSchedule {
		return nil
	}

	var active *catalog.Trigger
	for index := range action.Triggers {
		trigger := &action.Triggers[index]
		if trigger.Name == source.Schedule.TriggerName && trigger.Schedule != nil {
			active = trigger
			break
		}
	}
	if active == nil {
		return ErrScheduleNotFound
	}

	schedule, err := cronexpr.Parse(active.Schedule.Cron)
	if err != nil {
		return fmt.Errorf("%w: active cron is invalid", ErrInvalidRequest)
	}
	scheduledAt := source.Schedule.ScheduledAt.UTC()
	if !schedule.Next(scheduledAt.Add(-time.Minute)).Equal(scheduledAt) {
		return fmt.Errorf("%w: occurrence does not match the active cron", ErrScheduleNotFound)
	}

	expected, err := catalog.NormalizeAndValidateParameterValues(action.Parameters, active.Parameters)
	if err != nil {
		return invalidParameters(err)
	}
	if !reflect.DeepEqual(parameters, expected) {
		return fmt.Errorf("%w: values do not match the active scheduled trigger", ErrInvalidParameters)
	}

	return nil
}
