package catalog

import (
	"strings"
	"testing"
	"time"
)

func TestCompareReconciliation(t *testing.T) {
	t.Parallel()

	desired := strings.Repeat("a", 40)
	matching := desired
	different := strings.Repeat("b", 40)
	syncTarget := Target{Spec: TargetSpec{Actions: map[string]Action{"sync": {}}}}
	withoutSync := Target{Spec: TargetSpec{Actions: map[string]Action{"preview": {}}}}
	archivedAt := time.Now()

	tests := []struct {
		name   string
		record TargetSummary
		want   ReconciliationComparison
	}{
		{
			name: "matched",
			record: TargetSummary{
				Target: syncTarget, DesiredCommitSHA: desired,
				LastSuccessfulSync: &SyncRunCheckpoint{RelevanceCommitSHA: &matching},
			},
			want: ReconciliationComparisonMatched,
		},
		{
			name: "different",
			record: TargetSummary{
				Target: syncTarget, DesiredCommitSHA: desired,
				LastSuccessfulSync: &SyncRunCheckpoint{RelevanceCommitSHA: &different},
			},
			want: ReconciliationComparisonDifferent,
		},
		{
			name:   "no successful checkpoint",
			record: TargetSummary{Target: syncTarget, DesiredCommitSHA: desired},
			want:   ReconciliationComparisonUnknown,
		},
		{
			name: "unknown historical marker",
			record: TargetSummary{
				Target: syncTarget, DesiredCommitSHA: desired,
				LastSuccessfulSync: &SyncRunCheckpoint{},
			},
			want: ReconciliationComparisonUnknown,
		},
		{
			name: "no current sync action",
			record: TargetSummary{
				Target: withoutSync, DesiredCommitSHA: desired,
				LastSuccessfulSync: &SyncRunCheckpoint{RelevanceCommitSHA: &matching},
			},
			want: ReconciliationComparisonUnknown,
		},
		{
			name: "archived",
			record: TargetSummary{
				Target: syncTarget, DesiredCommitSHA: desired, ArchivedAt: &archivedAt,
				LastSuccessfulSync: &SyncRunCheckpoint{RelevanceCommitSHA: &matching},
			},
			want: ReconciliationComparisonUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.record.ReconciliationComparison(); got != test.want {
				t.Errorf("ReconciliationComparison() = %q, want %q", got, test.want)
			}
		})
	}
}
