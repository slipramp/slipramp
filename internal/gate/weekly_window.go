package gate

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // Minimal Server images still need the IANA database.
)

const (
	WindowType         = "window"
	minutesPerDay      = 24 * 60
	minutesPerWeek     = 7 * minutesPerDay
	maxWeeklyIntervals = 128
)

var ErrInvalidWindow = errors.New("invalid weekly window")

type WeeklyWindow struct {
	TimeZone string           `json:"timeZone"`
	Weekly   []WeeklyInterval `json:"weekly"`
}

type WeeklyInterval struct {
	Days  []string `json:"days"`
	Start string   `json:"start"`
	End   string   `json:"end"`
}

func (w *WeeklyWindow) UnmarshalJSON(data []byte) error {
	type fields WeeklyWindow
	return decodePayload(data, (*fields)(w))
}

type minuteSpan struct{ start, end int }

type WindowSchedule struct {
	Definition WeeklyWindow
	location   *time.Location
	spans      []minuteSpan
}

var weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func (w WeeklyWindow) Compile() (WindowSchedule, error) {
	var schedule WindowSchedule
	zone := w.TimeZone
	if zone == "" || len(zone) > 255 || zone == "Local" || zone == "localtime" || zone == "posixrules" ||
		strings.HasPrefix(zone, "posix/") || strings.HasPrefix(zone, "right/") {
		return schedule, fmt.Errorf("%w: expected an IANA time zone", ErrInvalidWindow)
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return schedule, fmt.Errorf("%w: unknown time zone %q", ErrInvalidWindow, zone)
	}
	if len(w.Weekly) == 0 || len(w.Weekly) > maxWeeklyIntervals {
		return schedule, fmt.Errorf("%w: expected 1 to %d weekly intervals", ErrInvalidWindow, maxWeeklyIntervals)
	}
	var byDay [7][]minuteSpan
	count := 0
	for _, interval := range w.Weekly {
		start, okStart := parseClockMinute(interval.Start)
		end, okEnd := parseClockMinute(interval.End)
		count += len(interval.Days)
		if !okStart || !okEnd || start >= end {
			return schedule, fmt.Errorf("%w: expected increasing HH:MM bounds within one day", ErrInvalidWindow)
		}
		if len(interval.Days) == 0 || len(interval.Days) > 7 || count > maxWeeklyIntervals {
			return schedule, fmt.Errorf("%w: expected 1 to 7 days per interval and at most %d day intervals", ErrInvalidWindow, maxWeeklyIntervals)
		}
		for _, day := range interval.Days {
			i := slices.Index(weekdays, day)
			if i < 0 {
				return schedule, fmt.Errorf("%w: unknown weekday %q", ErrInvalidWindow, day)
			}
			byDay[i] = append(byDay[i], minuteSpan{start, end})
		}
	}
	schedule.location = location
	schedule.Definition.TimeZone = zone
	for i, spans := range byDay {
		slices.SortFunc(spans, func(a, b minuteSpan) int { return a.start - b.start })
		var merged []minuteSpan
		for _, span := range spans {
			merged = appendSpan(merged, span)
		}
		for _, span := range merged {
			schedule.Definition.Weekly = append(schedule.Definition.Weekly, WeeklyInterval{
				Days: []string{weekdays[i]}, Start: formatMinute(span.start), End: formatMinute(span.end),
			})
			schedule.spans = appendSpan(schedule.spans, minuteSpan{i*minutesPerDay + span.start, i*minutesPerDay + span.end})
		}
	}
	// Join Sunday night to Monday morning before resolving local clock boundaries.
	n := len(schedule.spans)
	if n > 1 && schedule.spans[0].start == 0 && schedule.spans[n-1].end == minutesPerWeek {
		schedule.spans[n-1].end += schedule.spans[0].end
		schedule.spans = schedule.spans[1:]
	}
	return schedule, nil
}

func appendSpan(spans []minuteSpan, span minuteSpan) []minuteSpan {
	n := len(spans)
	if n > 0 && span.start <= spans[n-1].end {
		spans[n-1].end = max(spans[n-1].end, span.end)
		return spans
	}
	return append(spans, span)
}

func parseClockMinute(value string) (int, bool) {
	if value == "24:00" {
		return minutesPerDay, true
	}
	if len(value) != 5 {
		return 0, false
	}
	t, err := time.Parse("15:04", value)
	return t.Hour()*60 + t.Minute(), err == nil
}

func formatMinute(minute int) string { return fmt.Sprintf("%02d:%02d", minute/60, minute%60) }

type WindowObservation struct {
	State          RequirementState `json:"state"`
	EvaluatedAt    time.Time        `json:"evaluatedAt"`
	ValidUntil     time.Time        `json:"validUntil"`
	IntervalStart  *time.Time       `json:"intervalStart,omitempty"`
	NextTransition *time.Time       `json:"nextTransition,omitempty"`
}

// Evaluate resolves a bounded set of weekly occurrences. A missing boundary skips that
// occurrence. At a repeated clock time, use the later opening or the earlier closing.
func (s WindowSchedule) Evaluate(at time.Time) WindowObservation {
	at = at.UTC()
	result := WindowObservation{State: RequirementBlocking, EvaluatedAt: at, ValidUntil: at.AddDate(0, 0, 7)}
	if len(s.spans) == 1 && s.spans[0] == (minuteSpan{0, minutesPerWeek}) {
		result.State = RequirementPassing
		return result
	}
	local := at.In(s.location)
	date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	monday := date.AddDate(0, 0, -((int(local.Weekday()) + 6) % 7))
	for week := -2; week <= 2; week++ {
		base := monday.AddDate(0, 0, week*7)
		for _, span := range s.spans {
			open := s.resolveBoundary(base.Add(time.Duration(span.start)*time.Minute), true)
			closeAt := s.resolveBoundary(base.Add(time.Duration(span.end)*time.Minute), false)
			if open.IsZero() || !open.Before(closeAt) || !at.Before(closeAt) {
				continue
			}
			if !at.Before(open) {
				result.State = RequirementPassing
				result.IntervalStart, result.NextTransition, result.ValidUntil = &open, &closeAt, closeAt
				return result
			}
			if result.NextTransition == nil || open.Before(*result.NextTransition) {
				result.NextTransition, result.ValidUntil = &open, open
			}
		}
	}
	return result
}

// wall is a civil reading on a UTC calendar. Test it against each zone offset
// in a two-day neighborhood to find every real instant with that local reading.
func (s WindowSchedule) resolveBoundary(wall time.Time, opening bool) time.Time {
	var chosen time.Time
	limit := wall.Add(48 * time.Hour)
	for probe := wall.Add(-48 * time.Hour); !probe.After(limit); {
		local := probe.In(s.location)
		_, offset := local.Zone()
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		_, end := local.ZoneBounds()
		_, actualOffset := candidate.In(s.location).Zone()
		if actualOffset == offset {
			if chosen.IsZero() || (opening && candidate.After(chosen)) || (!opening && candidate.Before(chosen)) {
				chosen = candidate
			}
		}
		if end.IsZero() {
			break
		}
		if !end.After(probe) {
			// Some POSIX future rules use an artificial end-of-year boundary.
			probe = probe.Add(time.Hour)
		} else {
			probe = end
		}
	}
	return chosen
}
