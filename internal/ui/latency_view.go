package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/balia/gomodeltui/internal/latency"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderLatencyScreen() string {
	summaries := m.latencyStore.Summaries()
	header := lipgloss.NewStyle().Bold(true).Render("Latency history") + mutedStyle.Render("  l/Esc back  ↑↓ select  Home/End")
	if len(summaries) == 0 {
		return strings.Join([]string{header, "", mutedStyle.Render("No completed request timings observed yet.")}, "\n")
	}
	selected := min(m.latencySelected, len(summaries)-1)
	topRows := max(1, m.height/2-2)
	var list []string
	list = append(list, mutedStyle.Render("provider/model                                      attempts  logical  p50     p95     max"))
	start := 0
	if selected >= topRows {
		start = selected - topRows + 1
	}
	end := min(len(summaries), start+topRows)
	for index := start; index < end; index++ {
		summary := summaries[index]
		line := fmt.Sprintf("%-48s %8d  %7d  %-7s %-7s %-7s", truncateText(summary.Key, 48), summary.Attempts, summary.LogicalRequests, formatLatency(latency.Percentile(summary.Durations, 50)), formatLatency(latency.Percentile(summary.Durations, 95)), formatLatency(maxDuration(summary.Durations)))
		line = truncateText(line, max(1, m.width))
		line += strings.Repeat(" ", max(0, m.width-lipgloss.Width(line)))
		if index == selected {
			line = renderSelectedLine(line)
		}
		list = append(list, line)
	}

	selectedSummary := summaries[selected]
	histHeight := max(3, m.height-len(list)-4)
	histogram := renderLatencyHistogram(m.latencyStore.Histogram(selectedSummary.Key, max(1, m.width-8)), m.width, histHeight)
	chartHeader := fmt.Sprintf("%s  p50 %s  p95 %s  max %s", selectedSummary.Key, formatLatency(latency.Percentile(selectedSummary.Durations, 50)), formatLatency(latency.Percentile(selectedSummary.Durations, 95)), formatLatency(maxDuration(selectedSummary.Durations)))
	return strings.Join(append(append([]string{header}, list...), chartHeader, histogram), "\n")
}

func renderLatencyHistogram(buckets []int, width, height int) string {
	if len(buckets) == 0 || width < 1 || height < 1 {
		return ""
	}
	maxCount := 1
	for _, count := range buckets {
		if count > maxCount {
			maxCount = count
		}
	}
	if len(buckets) > width {
		buckets = buckets[len(buckets)-width:]
	}
	var lines []string
	for row := height; row > 0; row-- {
		var line strings.Builder
		for _, count := range buckets {
			if count*height/maxCount >= row {
				line.WriteString(failoverStyle.Render("█"))
			} else {
				line.WriteByte(' ')
			}
		}
		lines = append(lines, line.String())
	}
	axis := strings.Repeat("─", min(width, len(buckets)))
	lines = append(lines, axis)
	return strings.Join(lines, "\n")
}

func formatLatency(duration time.Duration) string {
	if duration <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fms", float64(duration)/float64(time.Millisecond))
}

func maxDuration(values []time.Duration) time.Duration {
	var result time.Duration
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}
