package latency

import (
	"testing"
	"time"
)

func TestStoreSummariesSortByAttemptsAndTrackLogicalRequests(t *testing.T) {
	store := NewStore()
	store.AddRequest([]Sample{{Key: "a/m", Duration: time.Second}, {Key: "b/m", Duration: 2 * time.Second}})
	store.AddRequest([]Sample{{Key: "a/m", Duration: 3 * time.Second}})
	summaries := store.Summaries()
	if len(summaries) != 2 || summaries[0].Key != "a/m" || summaries[0].Attempts != 2 || summaries[0].LogicalRequests != 2 {
		t.Fatalf("summaries=%+v", summaries)
	}
	if summaries[1].LogicalRequests != 1 {
		t.Fatalf("logical requests=%+v", summaries)
	}
}

func TestHistogramAndPercentile(t *testing.T) {
	store := NewStore()
	store.AddRequest([]Sample{{Key: "a/m", Duration: time.Second}, {Key: "a/m", Duration: 2 * time.Second}, {Key: "a/m", Duration: 4 * time.Second}})
	histogram := store.Histogram("a/m", 4)
	if histogram[0] != 1 || histogram[1] != 1 || histogram[3] != 1 {
		t.Fatalf("histogram=%v", histogram)
	}
	summary := store.Summaries()[0]
	if got := Percentile(summary.Durations, 50); got != 2*time.Second {
		t.Fatalf("p50=%s", got)
	}
}
