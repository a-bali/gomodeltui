package chart

import (
	"testing"
	"time"
)

func TestStoreSnapshotZeroFillsAndAggregates(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 5, 42, 0, time.UTC).Truncate(time.Minute)
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

func TestStoreSnapshotUsesRequestedColumnWidth(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 5, 42, 0, time.UTC)
	store := NewStore()
	store.Add(now.Add(-4*time.Minute-59*time.Second), true)
	store.Add(now.Add(-2*time.Minute-30*time.Second), false)
	buckets := store.Snapshot(now, Window5m, 100)
	if len(buckets) != 100 {
		t.Fatalf("got %d buckets", len(buckets))
	}
	if buckets[0].Success != 1 || buckets[50].Errors != 1 {
		t.Fatalf("unexpected dynamic buckets: first=%+v middle=%+v", buckets[0], buckets[50])
	}
}

func TestMaxTotalAndNextWindow(t *testing.T) {
	if got := MaxTotal([]Bucket{{Success: 2}, {Errors: 5}}); got != 5 {
		t.Fatalf("max=%d", got)
	}
	if got := NextWindow(Window1h, 1); got != Window3h {
		t.Fatalf("next=%v", got)
	}
	if got := NextWindow(Window5m, -1); got != Window5m {
		t.Fatalf("lowest=%v", got)
	}
	if got := NextWindow(Window15m, -1); got != Window5m {
		t.Fatalf("lower=%v", got)
	}
	if got := NextWindow(Window24h, 1); got != Window24h {
		t.Fatalf("upper=%v", got)
	}
}

func TestStorePrune(t *testing.T) {
	now := time.Now()
	store := NewStore()
	store.Add(now.Add(-2*time.Hour), true)
	store.Add(now.Add(-30*time.Minute), false)
	store.Prune(now.Add(-time.Hour))
	if len(store.events) != 1 || store.events[0].success {
		t.Fatalf("events=%+v", store.events)
	}
}
