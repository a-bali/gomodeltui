package latency

import (
	"sort"
	"time"
)

type Sample struct {
	Key          string
	At           time.Time
	Duration     time.Duration
	Success      bool
	InputTokens  int
	OutputTokens int
}

type Summary struct {
	Key             string
	Attempts        int
	LogicalRequests int
	Success         int
	Errors          int
	Durations       []time.Duration
}

type Store struct {
	samples  []Sample
	requests map[string]int
}

func NewStore() *Store {
	return &Store{requests: make(map[string]int)}
}

func (s *Store) AddRequest(samples []Sample) {
	if len(samples) == 0 {
		return
	}
	seen := make(map[string]bool)
	for _, sample := range samples {
		if sample.Key == "" || sample.Duration <= 0 {
			continue
		}
		s.samples = append(s.samples, sample)
		seen[sample.Key] = true
	}
	for key := range seen {
		s.requests[key]++
	}
}

func (s *Store) Summaries() []Summary {
	byKey := make(map[string]*Summary)
	for _, sample := range s.samples {
		summary := byKey[sample.Key]
		if summary == nil {
			summary = &Summary{Key: sample.Key}
			byKey[sample.Key] = summary
		}
		summary.Attempts++
		if sample.Success {
			summary.Success++
		} else {
			summary.Errors++
		}
		summary.Durations = append(summary.Durations, sample.Duration)
	}
	result := make([]Summary, 0, len(byKey))
	for _, summary := range byKey {
		summary.LogicalRequests = s.requests[summary.Key]
		sort.Slice(summary.Durations, func(i, j int) bool { return summary.Durations[i] < summary.Durations[j] })
		result = append(result, *summary)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Attempts != result[j].Attempts {
			return result[i].Attempts > result[j].Attempts
		}
		return result[i].Key < result[j].Key
	})
	return result
}

func (s *Store) Histogram(key string, buckets int) []int {
	return s.HistogramWithMax(key, buckets, RoundedMaxDuration(s.MaxDuration(), buckets))
}

func (s *Store) HistogramWithMax(key string, buckets int, roundedMax time.Duration) []int {
	if buckets < 1 {
		buckets = 1
	}
	var durations []time.Duration
	for _, sample := range s.samples {
		if sample.Key == key && sample.Duration > 0 {
			durations = append(durations, sample.Duration)
		}
	}
	result := make([]int, buckets)
	if len(durations) == 0 {
		return result
	}
	if roundedMax <= 0 {
		return result
	}
	bucketWidth := roundedMax / time.Duration(buckets)
	for _, duration := range durations {
		index := int(duration / bucketWidth)
		if index >= buckets {
			index = buckets - 1
		}
		result[index]++
	}
	return result
}

func RoundedMaxDuration(maxDuration time.Duration, buckets int) time.Duration {
	if maxDuration <= 0 || buckets < 1 {
		return 0
	}
	unit := time.Second
	for maxDuration >= unit*10 {
		unit *= 10
	}
	normalized := float64(maxDuration) / float64(unit)
	multiplier := 1.0
	for _, candidate := range []float64{1, 2, 5, 10} {
		if normalized <= candidate {
			multiplier = candidate
			break
		}
	}
	return time.Duration(multiplier * float64(unit))
}

func (s *Store) MaxDuration() time.Duration {
	var result time.Duration
	for _, sample := range s.samples {
		if sample.Duration > result {
			result = sample.Duration
		}
	}
	return result
}

func Percentile(durations []time.Duration, percentile int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	if percentile < 0 {
		percentile = 0
	}
	if percentile > 100 {
		percentile = 100
	}
	index := (len(durations) - 1) * percentile / 100
	return durations[index]
}
