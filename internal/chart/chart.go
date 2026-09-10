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
	buckets map[time.Time]Bucket
}

func NewStore() *Store { return &Store{buckets: make(map[time.Time]Bucket)} }

func (s *Store) Add(at time.Time, success bool) {
	start := at.Truncate(time.Minute)
	bucket := s.buckets[start]
	bucket.Start = start
	if success {
		bucket.Success++
	} else {
		bucket.Errors++
	}
	s.buckets[start] = bucket
}

func (s *Store) Snapshot(now time.Time, window Window) []Bucket {
	end := now.Truncate(time.Minute)
	start := end.Add(-time.Duration(window) + time.Minute)
	result := make([]Bucket, 0, int(time.Duration(window)/time.Minute))
	for at := start; !at.After(end); at = at.Add(time.Minute) {
		result = append(result, s.buckets[at])
		result[len(result)-1].Start = at
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
