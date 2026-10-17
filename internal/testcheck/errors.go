// Package testcheck provides focused assertions shared by Slipramp tests.
package testcheck

import (
	"strings"
	"testing"
)

// ErrorContains checks that err is nil when want is empty, or contains want.
func ErrorContains(t testing.TB, err error, want string) {
	t.Helper()

	if want == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		return
	}

	if err == nil {
		t.Fatalf("error = nil, want an error containing %q", want)
	}

	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want an error containing %q", err, want)
	}
}
