package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/balia/gomodeltui/internal/latency"
	"github.com/charmbracelet/lipgloss"
)

const latencyHistogramBuckets = 10

func (m Model) renderLatencyScreen() string {
	summaries := m.latencyStore.Summaries()
	header := lipgloss.NewStyle().Bold(true).Render("Latency history") + mutedStyle.Render("  l/Esc back  ↑↓/PgUp/PgDn select  Home/End")
	if len(summaries) == 0 {
		return strings.Join([]string{header, "", mutedStyle.Render("No completed request timings observed yet.")}, "\n")
	}
	selected := min(m.latencySelected, len(summaries)-1)
	topRows := latencyListRows(m.height)
	var list []string
	list = append(list, mutedStyle.Render("provider/model                              attempts logical  ok  err  p50     p95     max"))
	start := min(m.latencyOffset, max(0, len(summaries)-topRows))
	end := min(len(summaries), start+topRows)
	for index := start; index < end; index++ {
		summary := summaries[index]
		line := fmt.Sprintf("%-40s %8d %7d %3d %4d  %-7s %-7s %-7s", truncateText(summary.Key, 40), summary.Attempts, summary.LogicalRequests, summary.Success, summary.Errors, formatLatency(latency.Percentile(summary.Durations, 50)), formatLatency(latency.Percentile(summary.Durations, 95)), formatLatency(maxDuration(summary.Durations)))
		line = truncateText(line, max(1, m.width))
		line += strings.Repeat(" ", max(0, m.width-lipgloss.Width(line)))
		if index == selected {
			line = renderSelectedLine(line)
		}
		list = append(list, line)
	}

	selectedSummary := summaries[selected]
	bottomHeight := max(3, m.height-latencyTopRows(m.height)-1)
	labels := latencyBucketLabels(m.latencyStore.MaxDuration(), latencyHistogramBuckets)
	histogram := renderCenteredLatencyHistogram(m.latencyStore.Histogram(selectedSummary.Key, latencyHistogramBuckets), labels, m.width, bottomHeight)
	return strings.Join(append([]string{header}, append(list, histogram)...), "\n")
}

func latencyTopRows(height int) int { return max(3, height*40/100) }

func latencyListRows(height int) int { return max(1, latencyTopRows(height)-2) }

func latencyListOffset(selected, offset, total, height int) int {
	rows := latencyListRows(height)
	if selected < offset {
		offset = selected
	}
	if selected >= offset+rows {
		offset = selected - rows + 1
	}
	return min(max(0, offset), max(0, total-rows))
}

func renderCenteredLatencyHistogram(buckets []int, labels []string, width, height int) string {
	if len(buckets) == 0 || width < 1 || height < 1 {
		return ""
	}
	barWidth := max(2, min(8, (width-len(buckets)+1)/len(buckets)))
	maxCount := 1
	for _, count := range buckets {
		maxCount = max(maxCount, count)
	}
	barHeight := max(1, height-2)
	var lines []string
	for row := barHeight; row > 0; row-- {
		var line strings.Builder
		for index, count := range buckets {
			glyph := " "
			if count*barHeight/maxCount >= row {
				glyph = failoverStyle.Render(strings.Repeat("█", barWidth))
			}
			line.WriteString(glyph)
			if index+1 < len(buckets) {
				line.WriteByte(' ')
			}
		}
		lines = append(lines, centerLine(line.String(), width))
	}
	var labelLine strings.Builder
	for index, label := range labels {
		labelLine.WriteString(centerText(truncateText(label, barWidth), barWidth))
		if index+1 < len(labels) {
			labelLine.WriteByte(' ')
		}
	}
	lines = append(lines, centerLine(labelLine.String(), width))
	padding := max(0, (height-len(lines))/2)
	if padding > 0 {
		lines = append(make([]string, padding), lines...)
	}
	return strings.Join(lines, "\n")
}

func latencyBucketLabels(maxDuration time.Duration, buckets int) []string {
	labels := make([]string, buckets)
	if maxDuration <= 0 {
		return labels
	}
	for index := range labels {
		upper := maxDuration * time.Duration(index+1) / time.Duration(buckets)
		labels[index] = formatLatency(upper)
	}
	return labels
}

func centerLine(line string, width int) string {
	return strings.Repeat(" ", max(0, (width-lipgloss.Width(line))/2)) + line
}

func centerText(text string, width int) string {
	return strings.Repeat(" ", max(0, (width-lipgloss.Width(text))/2)) + text + strings.Repeat(" ", max(0, width-lipgloss.Width(text)-max(0, (width-lipgloss.Width(text))/2)))
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
