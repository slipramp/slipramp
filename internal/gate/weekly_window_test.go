package gate

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWeeklyWindowBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, zone, day, start, end, at string
		open                            bool
		next                            string
	}{
		{"before", "UTC", "monday", "09:00", "17:00", "2026-11-23T08:59:59Z", false, "2026-11-23T09:00:00Z"},
		{"opening", "UTC", "monday", "09:00", "17:00", "2026-11-23T09:00:00Z", true, "2026-11-23T17:00:00Z"},
		{"closing", "UTC", "monday", "09:00", "17:00", "2026-11-23T17:00:00Z", false, "2026-11-30T09:00:00Z"},
		{"missing start", "America/Denver", "sunday", "02:15", "04:00", "2026-03-08T09:30:00Z", false, "2026-03-15T08:15:00Z"},
		{"missing end", "America/Denver", "sunday", "00:00", "02:15", "2026-03-08T08:30:00Z", false, "2026-03-15T06:00:00Z"},
		{"first opening skipped", "America/Denver", "sunday", "01:30", "03:00", "2026-11-01T07:45:00Z", false, "2026-11-01T08:30:00Z"},
		{"later opening", "America/Denver", "sunday", "01:30", "03:00", "2026-11-01T08:30:00Z", true, "2026-11-01T10:00:00Z"},
		{"earlier close", "America/Denver", "sunday", "00:00", "01:30", "2026-11-01T07:29:00Z", true, "2026-11-01T07:30:00Z"},
		{"fractional offset", "Asia/Kathmandu", "monday", "09:00", "10:00", "2026-11-23T03:15:00Z", true, "2026-11-23T04:15:00Z"},
		{"half-hour fold", "Australia/Lord_Howe", "sunday", "01:45", "03:00", "2026-04-04T14:50:00Z", false, "2026-04-04T15:15:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := (WeeklyWindow{tc.zone, []WeeklyInterval{{[]string{tc.day}, tc.start, tc.end}}}).Compile()
			if err != nil {
				t.Fatal(err)
			}
			at, err := time.Parse(time.RFC3339, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			o := s.Evaluate(at)
			if (o.State == RequirementPassing) != tc.open || o.NextTransition == nil || o.NextTransition.Format(time.RFC3339) != tc.next ||
				!o.ValidUntil.Equal(*o.NextTransition) || (o.IntervalStart != nil) != tc.open {
				t.Fatalf("at %s: %#v", at, o)
			}
		})
	}
}

func TestWeeklyWindowNormalization(t *testing.T) {
	w := WeeklyWindow{"UTC", []WeeklyInterval{
		{[]string{"monday"}, "00:00", "02:00"}, {[]string{"sunday"}, "23:00", "24:00"},
		{[]string{"monday"}, "01:00", "03:00"},
	}}
	s, err := w.Compile()
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Definition.Compile()
	if err != nil || !reflect.DeepEqual(again.Definition, s.Definition) || len(s.spans) != 1 {
		t.Fatalf("normalized = %#v, %v", again, err)
	}
	at := time.Date(2026, 11, 23, 0, 30, 0, 0, time.UTC)
	o := s.Evaluate(at)
	if o.State != RequirementPassing || !o.IntervalStart.Equal(at.Add(-90*time.Minute)) || !o.ValidUntil.Equal(at.Add(150*time.Minute)) {
		t.Fatal(o)
	}
	full, err := (WeeklyWindow{"UTC", []WeeklyInterval{{weekdays, "00:00", "24:00"}}}).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if o := full.Evaluate(at); o.State != RequirementPassing || o.NextTransition != nil || !o.ValidUntil.After(at) {
		t.Fatal(o)
	}
}

func TestRejectInvalidWeeklyWindows(t *testing.T) {
	good := WeeklyInterval{[]string{"monday"}, "09:00", "17:00"}
	for _, w := range []WeeklyWindow{
		{"Local", []WeeklyInterval{good}}, {"Mars/Base", []WeeklyInterval{good}}, {"UTC", nil},
		{"UTC", []WeeklyInterval{{nil, "09:00", "17:00"}}}, {"UTC", []WeeklyInterval{{[]string{"mon"}, "09:00", "17:00"}}},
		{"UTC", []WeeklyInterval{{weekdays, "22:00", "04:00"}}}, {"UTC", []WeeklyInterval{{weekdays, "09:00", "09:00"}}},
		{"UTC", []WeeklyInterval{{weekdays, "9:00", "17:00"}}}, {"UTC", make([]WeeklyInterval, maxWeeklyIntervals+1)},
	} {
		if _, err := w.Compile(); !errors.Is(err, ErrInvalidWindow) {
			t.Fatalf("accepted %#v: %v", w, err)
		}
	}
}

func TestWindowEvidenceAndFreshness(t *testing.T) {
	at := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	next := at.Add(time.Hour)
	for _, state := range []RequirementState{RequirementPassing, RequirementBlocking} {
		o := WindowObservation{State: state, EvaluatedAt: at, NextTransition: &next, ValidUntil: next}
		if o.StateAt(at) != state || o.StateAt(at.Add(-time.Nanosecond)) != RequirementUnavailable ||
			o.StateAt(next) != RequirementUnavailable {
			t.Fatalf("freshness for %s", state)
		}
		w := &WindowEvidence{TimeZone: "UTC", Digest: strings.Repeat("a", 64), Observation: o}
		encoded, err := json.Marshal(Evidence{Payload: w})
		if err != nil {
			t.Fatal(err)
		}
		var decoded Evidence
		if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(w, decoded.Payload) {
			t.Fatalf("decoded = %#v, %v", decoded, err)
		}
	}
}
