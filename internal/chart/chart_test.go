package chart

import (
	"testing"
	"time"
)

func TestStoreSnapshotZeroFillsAndAggregates(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 5, 42, 0, time.UTC)
	store := NewStore()
	store.Add(now.Add(-2*time.Minute), true)
	store.Add(now.Add(-2*time.Minute), false)
	buckets := store.Snapshot(now, Window15m)
	if len(buckets) != 15 {
		t.Fatalf("got %d buckets", len(buckets))
	}
	if buckets[12].Success != 1 || buckets[12].Errors != 1 {
		t.Fatalf("unexpected bucket: %+v", buckets[12])
	}
	if buckets[0].Total() != 0 || buckets[14].Total() != 0 {
		t.Fatal("expected zero-filled edges")
	}
}

func TestMaxTotalAndNextWindow(t *testing.T) {
	if got := MaxTotal([]Bucket{{Success: 2}, {Errors: 5}}); got != 5 {
		t.Fatalf("max=%d", got)
	}
	if got := NextWindow(Window1h, 1); got != Window3h {
		t.Fatalf("next=%v", got)
	}
	if got := NextWindow(Window15m, -1); got != Window15m {
		t.Fatalf("lower=%v", got)
	}
	if got := NextWindow(Window24h, 1); got != Window24h {
		t.Fatalf("upper=%v", got)
	}
}
