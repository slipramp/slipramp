package promotion

import (
	"testing"
	"time"
)

func TestEvaluateFreightReadiness(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.November, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		request   FreightRequest
		candidate FreightItem
		current   bool
		want      FreightReadiness
	}{
		{
			name:      "unrequested origin",
			candidate: FreightItem{Origin: "Warehouse/app", Name: "freight-1"},
			want:      FreightReadinessUnavailable,
		},
		{
			name:      "direct origin",
			request:   FreightRequest{Origin: "Warehouse/app", Direct: true},
			candidate: FreightItem{Origin: "Warehouse/app", Name: "freight-1"},
			want:      FreightReadinessReady,
		},
		{
			name:      "already current",
			request:   FreightRequest{Origin: "Warehouse/app", Direct: true},
			candidate: FreightItem{Origin: "Warehouse/app", Name: "freight-1"},
			current:   true,
			want:      FreightReadinessUnavailable,
		},
		{
			name: "missing upstream evidence",
			request: FreightRequest{
				Origin: "Warehouse/app", SourceStages: []string{"test"},
			},
			candidate: FreightItem{Origin: "Warehouse/app", Name: "freight-1"},
			want:      FreightReadinessUnknown,
		},
		{
			name: "explicit approval",
			request: FreightRequest{
				Origin: "Warehouse/app", SourceStages: []string{"test"}, RequiredSoakTime: time.Hour,
			},
			candidate: FreightItem{
				Origin: "Warehouse/app", Name: "freight-1", EvidenceObserved: true,
				ApprovedFor: []string{"prod"},
			},
			want: FreightReadinessReady,
		},
		{
			name: "explicit approval is sufficient partial evidence",
			request: FreightRequest{
				Origin: "Warehouse/app", SourceStages: []string{"test"}, RequiredSoakTime: time.Hour,
			},
			candidate: FreightItem{
				Origin: "Warehouse/app", Name: "freight-1", ApprovedFor: []string{"prod"},
			},
			want: FreightReadinessReady,
		},
		{
			name: "one of verified and soaked",
			request: FreightRequest{
				Origin: "Warehouse/app", SourceStages: []string{"test", "uat"},
				AvailabilityStrategy: FreightAvailabilityOneOf, RequiredSoakTime: time.Hour,
			},
			candidate: FreightItem{
				Origin: "Warehouse/app", Name: "freight-1", EvidenceObserved: true,
				VerifiedIn:  []FreightVerification{{Stage: "uat"}},
				CurrentlyIn: []FreightOccupancy{{Stage: "uat", Since: observedAt.Add(-time.Hour)}},
			},
			want: FreightReadinessReady,
		},
		{
			name: "all rejects a missing source",
			request: FreightRequest{
				Origin: "Warehouse/app", SourceStages: []string{"test", "uat"},
				AvailabilityStrategy: FreightAvailabilityAll,
			},
			candidate: FreightItem{
				Origin: "Warehouse/app", Name: "freight-1", EvidenceObserved: true,
				VerifiedIn: []FreightVerification{{Stage: "test"}},
			},
			want: FreightReadinessUnavailable,
		},
		{
			name: "all accepts every verified source",
			request: FreightRequest{
				Origin: "Warehouse/app", SourceStages: []string{"test", "uat"},
				AvailabilityStrategy: FreightAvailabilityAll,
			},
			candidate: FreightItem{
				Origin: "Warehouse/app", Name: "freight-1", EvidenceObserved: true,
				VerifiedIn: []FreightVerification{{Stage: "test"}, {Stage: "uat"}},
			},
			want: FreightReadinessReady,
		},
		{
			name: "soak requirement remains unavailable",
			request: FreightRequest{
				Origin: "Warehouse/app", SourceStages: []string{"test"}, RequiredSoakTime: 2 * time.Hour,
			},
			candidate: FreightItem{
				Origin: "Warehouse/app", Name: "freight-1", EvidenceObserved: true,
				VerifiedIn: []FreightVerification{{Stage: "test", LongestCompletedSoak: time.Hour}},
			},
			want: FreightReadinessUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			target := Stage{Name: "prod", LastObservedAt: observedAt}
			if test.request.Origin != "" {
				target.RequestedFreight = []FreightRequest{test.request}
			}
			if test.current {
				target.Freight.Items = []FreightItem{{Origin: test.candidate.Origin, Name: test.candidate.Name}}
			}
			if got := EvaluateFreightReadiness(target, test.candidate); got.Readiness != test.want {
				t.Errorf("EvaluateFreightReadiness() = %#v, want readiness %q", got, test.want)
			}
		})
	}
}
