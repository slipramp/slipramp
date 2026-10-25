package durationvalidate_test

import (
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/durationvalidate"
)

func TestPositive(t *testing.T) {
	t.Parallel()

	got, err := durationvalidate.Positive("timeout", "90s")
	if err != nil {
		t.Fatalf("Positive() error = %v", err)
	}
	if got != 90*time.Second {
		t.Errorf("Positive() = %s, want %s", got, 90*time.Second)
	}

	for _, value := range []string{"invalid", "0s", "-1s"} {
		if _, err := durationvalidate.Positive("timeout", value); err == nil {
			t.Errorf("Positive(%q) error = nil", value)
		}
	}
}
