package cluster

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		wantError   bool
	}{
		{name: "single character", clusterName: "a"},
		{name: "starts with digit", clusterName: "1-colo"},
		{
			name:        "maximum length",
			clusterName: "a" + strings.Repeat("2", 62),
		},
		{name: "empty", clusterName: "", wantError: true},
		{name: "uppercase", clusterName: "Colo", wantError: true},
		{name: "ends with hyphen", clusterName: "colo-", wantError: true},
		{
			name:        "too long",
			clusterName: "a" + strings.Repeat("2", 63),
			wantError:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateName(test.clusterName)
			if test.wantError && err == nil {
				t.Fatal("ValidateName() error = nil, want an error")
			}

			if !test.wantError && err != nil {
				t.Fatalf("ValidateName() error = %v", err)
			}
		})
	}
}
