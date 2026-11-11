package kubevalidate_test

import (
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

func TestResourceRequirements(t *testing.T) {
	t.Parallel()

	if err := kubevalidate.ResourceRequirements(
		"action",
		map[string]string{"cpu": "100m"},
		map[string]string{"cpu": "1"},
	); err != nil {
		t.Fatalf("ResourceRequirements() error = %v", err)
	}

	tests := []struct {
		name     string
		requests map[string]string
		limits   map[string]string
		contains string
	}{
		{
			name:     "invalid quantity",
			requests: map[string]string{"cpu": "many"},
			contains: "invalid quantity",
		},
		{
			name:     "negative quantity",
			requests: map[string]string{"cpu": "-1"},
			contains: "must not be negative",
		},
		{
			name:     "request above limit",
			requests: map[string]string{"memory": "2Gi"},
			limits:   map[string]string{"memory": "1Gi"},
			contains: "must not exceed its limit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := kubevalidate.ResourceRequirements("action", test.requests, test.limits)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("ResourceRequirements() error = %v, want containing %q", err, test.contains)
			}
		})
	}
}
