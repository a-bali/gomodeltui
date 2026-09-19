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
	contentWidth := max(1, m.width-1)
	var list []string
	list = append(list, mutedStyle.Render(renderLatencyTableHeader(contentWidth))+" ")
	start := min(m.latencyOffset, max(0, len(summaries)-topRows))
	end := min(len(summaries), start+topRows)
	thumbStart, thumbEnd := scrollbarThumb(topRows, len(summaries), start)
	for index := start; index < end; index++ {
		summary := summaries[index]
		line := renderLatencyTableRow(summary, contentWidth)
		line += strings.Repeat(" ", max(0, contentWidth-lipgloss.Width(line)))
		if index == selected {
			line = renderSelectedLine(line)
		}
		line += " " + scrollbarCell(index-start, thumbStart, thumbEnd)
		list = append(list, line)
	}

	selectedSummary := summaries[selected]
	bottomHeight := max(3, m.height-latencyTopRows(m.height)-1)
	scaleMax := m.latencyScaleMax
	if scaleMax <= 0 {
		scaleMax = latency.RoundedMaxDuration(m.latencyStore.MaxDuration(), latencyHistogramBuckets)
	}
	labels := latencyBucketLabels(scaleMax, latencyHistogramBuckets)
	histogram := renderCenteredLatencyHistogram(m.latencyStore.HistogramWithMax(selectedSummary.Key, latencyHistogramBuckets, scaleMax), labels, m.width, bottomHeight)
	return strings.Join(append([]string{header}, append(list, histogram)...), "\n")
}

const latencyMetricWidth = 8

func latencyModelColumnWidth(width int) int {
	// Seven metric columns and their separators occupy the remainder of the
	// table; model routing gets every other available terminal column.
	return max(1, width-(latencyMetricWidth*7)-7)
}

func renderLatencyTableHeader(width int) string {
	modelWidth := latencyModelColumnWidth(width)
	line := fmt.Sprintf("%-*s %*s %*s %*s %*s %*s %*s %*s",
		modelWidth, "provider/model",
		latencyMetricWidth, "attempts", latencyMetricWidth, "logical",
		latencyMetricWidth, "ok", latencyMetricWidth, "err",
		latencyMetricWidth, "p50", latencyMetricWidth, "p95", latencyMetricWidth, "max")
	return truncateText(line, width)
}

func renderLatencyTableRow(summary latency.Summary, width int) string {
	modelWidth := latencyModelColumnWidth(width)
	line := fmt.Sprintf("%-*s %*d %*d %*d %*d %*s %*s %*s",
		modelWidth, truncateText(summary.Key, modelWidth),
		latencyMetricWidth, summary.Attempts, latencyMetricWidth, summary.LogicalRequests,
		latencyMetricWidth, summary.Success, latencyMetricWidth, summary.Errors,
		latencyMetricWidth, formatLatency(latency.Percentile(summary.Durations, 50)),
		latencyMetricWidth, formatLatency(latency.Percentile(summary.Durations, 95)),
		latencyMetricWidth, formatLatency(maxDuration(summary.Durations)))
	return truncateText(line, width)
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
	barWidth := max(2, (width-len(buckets)+1)/len(buckets))
	maxCount := 1
	for _, count := range buckets {
		maxCount = max(maxCount, count)
	}
	barHeight := min(12, max(1, height-2))
	var lines []string
	for row := barHeight; row > 0; row-- {
		var line strings.Builder
		for index, count := range buckets {
			glyph := strings.Repeat(" ", barWidth)
			if count*barHeight/maxCount >= row {
				glyph = failoverStyle.Render(strings.Repeat("█", barWidth))
			}
			line.WriteString(glyph)
			if index+1 < len(buckets) {
				line.WriteString(mutedStyle.Render("│"))
			}
		}
		lines = append(lines, centerLine(line.String(), width))
	}
	var labelLine strings.Builder
	for index, label := range labels {
		labelLine.WriteString(centerText(truncateText(label, barWidth), barWidth))
		if index+1 < len(labels) {
			labelLine.WriteString(mutedStyle.Render("│"))
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
		labels[index] = formatBucketDuration(upper)
	}
	return labels
}

func formatBucketDuration(duration time.Duration) string {
	if duration >= time.Second {
		return fmt.Sprintf("%.0fs", duration.Seconds())
	}
	return fmt.Sprintf("%.0fms", float64(duration)/float64(time.Millisecond))
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
