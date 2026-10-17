package catalog

import "testing"

func TestTargetAffected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		patterns     []string
		changedPaths []string
		want         bool
	}{
		{
			name:         "exact path",
			patterns:     []string{"deploys/inventory.py"},
			changedPaths: []string{"deploys/inventory.py"},
			want:         true,
		},
		{
			name:         "single segment wildcard",
			patterns:     []string{"deploys/*.py"},
			changedPaths: []string{"deploys/inventory.py"},
			want:         true,
		},
		{
			name:         "single segment wildcard does not cross directory",
			patterns:     []string{"deploys/*.py"},
			changedPaths: []string{"deploys/shared/inventory.py"},
		},
		{
			name:         "recursive wildcard",
			patterns:     []string{"terraform/modules/**"},
			changedPaths: []string{"terraform/modules/network/main.tf"},
			want:         true,
		},
		{
			name:         "recursive wildcard matches direct child",
			patterns:     []string{"terraform/modules/**"},
			changedPaths: []string{"terraform/modules/main.tf"},
			want:         true,
		},
		{
			name:         "recursive wildcard between segments",
			patterns:     []string{"terraform/**/main.tf"},
			changedPaths: []string{"terraform/environments/home/main.tf"},
			want:         true,
		},
		{
			name:         "unrelated changes",
			patterns:     []string{"terraform/**"},
			changedPaths: []string{"docs/README.md", "deploys/app.yaml"},
		},
		{
			name:         "no watch patterns",
			changedPaths: []string{"terraform/main.tf"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			target := Target{Spec: TargetSpec{Watch: test.patterns}}
			if got := TargetAffected(target, test.changedPaths); got != test.want {
				t.Errorf("TargetAffected() = %t, want %t", got, test.want)
			}
		})
	}
}
