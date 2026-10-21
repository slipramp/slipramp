package controller

import (
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/api"
)

func TestShouldReportRunObservation(t *testing.T) {
	t.Parallel()

	reportedAt := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	reported := runObservation{phase: api.RunObservationPhasePreparing}
	running := runObservation{phase: api.RunObservationPhaseRunning}
	heartbeatEvery := 15 * time.Second
	tests := []struct {
		name        string
		observation runObservation
		reported    *runObservation
		now         time.Time
		want        bool
	}{
		{
			name:        "first observation",
			observation: reported,
			now:         reportedAt,
			want:        true,
		},
		{
			name:        "changed observation",
			observation: running,
			reported:    &reported,
			now:         reportedAt,
			want:        true,
		},
		{
			name:        "unchanged before heartbeat",
			observation: reported,
			reported:    &reported,
			now:         reportedAt.Add(heartbeatEvery - time.Nanosecond),
		},
		{
			name:        "unchanged at heartbeat",
			observation: reported,
			reported:    &reported,
			now:         reportedAt.Add(heartbeatEvery),
			want:        true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := shouldReport(
				test.observation,
				test.reported,
				reportedAt,
				test.now,
				heartbeatEvery,
			)
			if got != test.want {
				t.Errorf("shouldReport() = %t, want %t", got, test.want)
			}
		})
	}
}
