package chart

import (
	"sort"
	"time"
)

type Window time.Duration

const (
	Window5m  Window = Window(5 * time.Minute)
	Window15m Window = Window(15 * time.Minute)
	Window1h  Window = Window(time.Hour)
	Window3h  Window = Window(3 * time.Hour)
	Window6h  Window = Window(6 * time.Hour)
	Window12h Window = Window(12 * time.Hour)
	Window24h Window = Window(24 * time.Hour)
)

var Windows = []Window{Window5m, Window15m, Window1h, Window3h, Window6h, Window12h, Window24h}

type Bucket struct {
	Start   time.Time
	Success int
	Errors  int
}

func (b Bucket) Total() int { return b.Success + b.Errors }

type Store struct {
	events []event
}

type event struct {
	at      time.Time
	success bool
}

func NewStore() *Store { return &Store{} }

func (s *Store) Add(at time.Time, success bool) {
	s.events = append(s.events, event{at: at, success: success})
}

func (s *Store) Prune(before time.Time) {
	kept := s.events[:0]
	for _, event := range s.events {
		if !event.at.Before(before) {
			kept = append(kept, event)
		}
	}
	s.events = kept
}

// Snapshot returns exactly columns time buckets when columns is provided.
// With no column count it retains the historical one-minute resolution.
func (s *Store) Snapshot(now time.Time, window Window, columns ...int) []Bucket {
	width := int(time.Duration(window) / time.Minute)
	start := now.Truncate(time.Minute).Add(-time.Duration(window) + time.Minute)
	end := now.Truncate(time.Minute).Add(time.Minute - time.Nanosecond)
	if len(columns) > 0 && columns[0] > 0 {
		width = columns[0]
		start = now.Add(-time.Duration(window))
		end = now
	}
	if width < 1 {
		width = 1
	}
	duration := time.Duration(window) / time.Duration(width)
	result := make([]Bucket, width)
	for index := range result {
		result[index].Start = start.Add(time.Duration(index) * duration)
	}
	for _, event := range s.events {
		if event.at.Before(start) || event.at.After(end) {
			continue
		}
		index := int(event.at.Sub(start) / duration)
		if index >= width {
			index = width - 1
		}
		if event.success {
			result[index].Success++
		} else {
			result[index].Errors++
		}
	}
	return result
}

func MaxTotal(buckets []Bucket) int {
	max := 1
	for _, bucket := range buckets {
		if bucket.Total() > max {
			max = bucket.Total()
		}
	}
	return max
}

func NextWindow(current Window, direction int) Window {
	index := sort.Search(len(Windows), func(i int) bool { return Windows[i] >= current })
	if index >= len(Windows) {
		index = len(Windows) - 1
	}
	index += direction
	if index < 0 {
		index = 0
	}
	if index >= len(Windows) {
		index = len(Windows) - 1
	}
	return Windows[index]
}
