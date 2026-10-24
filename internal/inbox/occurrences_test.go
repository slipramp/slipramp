package inbox

import (
	"testing"
	"time"
)

func TestProblemOccurrenceID(t *testing.T) {
	t.Parallel()

	marker := time.Date(2026, time.October, 23, 12, 19, 20, 123456789, time.FixedZone("MDT", -6*60*60))
	got := problemOccurrenceID(KindRepositoryRefresh, "resource-1", marker)
	const want = "2b718c14eb60b7a0c1313b177cdfb5da1792932b7fc068811e485abf9108515a"
	if got != want {
		t.Fatalf("problemOccurrenceID() = %q, want %q", got, want)
	}

	equivalentTime := marker.UTC()
	if equivalent := problemOccurrenceID(KindRepositoryRefresh, "resource-1", equivalentTime); equivalent != got {
		t.Errorf("equivalent instant produced %q, want %q", equivalent, got)
	}
	if next := problemOccurrenceID(KindRepositoryRefresh, "resource-1", marker.Add(time.Nanosecond)); next == got {
		t.Error("new occurrence retained the previous ID")
	}
}
