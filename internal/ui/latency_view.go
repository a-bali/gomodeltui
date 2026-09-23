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
	header := lipgloss.NewStyle().Bold(true).Render("Latency history") + mutedStyle.Render(fmt.Sprintf("  +/- buckets:%d  c recalculate  l/Esc back  ↑↓/PgUp/PgDn select  Home/End", m.latencyBuckets))
	if len(summaries) == 0 {
		return strings.Join([]string{header, "", mutedStyle.Render("No completed request timings observed yet.")}, "\n")
	}
	selected := min(m.latencySelected, len(summaries)-1)
	topRows := latencyListRows(m.height)
	contentWidth := max(1, m.width-2)
	table := newLatencyTable(summaries, contentWidth)
	var list []string
	list = append(list, mutedStyle.Render(table.header())+strings.Repeat(" ", max(0, m.width-lipgloss.Width(table.header()))))
	start := min(m.latencyOffset, max(0, len(summaries)-topRows))
	end := min(len(summaries), start+topRows)
	thumbStart, thumbEnd := scrollbarThumb(topRows, len(summaries), start)
	for index := start; index < end; index++ {
		summary := summaries[index]
		line := table.row(summary)
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
		scaleMax = latency.RoundedMaxDuration(m.latencyStore.MaxDuration(), m.latencyBuckets)
	}
	labels := latencyBucketLabels(scaleMax, m.latencyBuckets)
	histogram := renderCenteredLatencyHistogram(m.latencyStore.HistogramWithMax(selectedSummary.Key, m.latencyBuckets, scaleMax), labels, m.width, bottomHeight)
	return strings.Join(append([]string{header}, append(list, histogram)...), "\n")
}

var latencyMetricHeaders = []string{"attempts", "logical", "ok", "err", "p50", "p95", "max"}

type latencyTable struct{ widths []int }

func newLatencyTable(summaries []latency.Summary, availableWidth int) latencyTable {
	widths := make([]int, len(latencyMetricHeaders)+1)
	widths[0] = len("provider/model")
	for index, header := range latencyMetricHeaders {
		widths[index+1] = len(header)
	}
	for _, summary := range summaries {
		values := latencyTableValues(summary)
		for index, value := range values {
			widths[index] = max(widths[index], lipgloss.Width(value))
		}
	}
	used := 2 * (len(widths) - 1)
	for _, width := range widths {
		used += width
	}
	widths[0] += max(0, availableWidth-used)
	return latencyTable{widths: widths}
}

func latencyTableValues(summary latency.Summary) []string {
	return []string{summary.Key,
		fmt.Sprintf("%d", summary.Attempts), fmt.Sprintf("%d", summary.LogicalRequests),
		fmt.Sprintf("%d", summary.Success), fmt.Sprintf("%d", summary.Errors),
		formatLatency(latency.Percentile(summary.Durations, 50)),
		formatLatency(latency.Percentile(summary.Durations, 95)),
		formatLatency(maxDuration(summary.Durations))}
}

func (t latencyTable) header() string {
	return t.format(append([]string{"provider/model"}, latencyMetricHeaders...))
}

func (t latencyTable) row(summary latency.Summary) string {
	return t.format(latencyTableValues(summary))
}

func (t latencyTable) format(values []string) string {
	columns := make([]string, len(values))
	for index, value := range values {
		if index == 0 {
			columns[index] = fmt.Sprintf("%-*s", t.widths[index], value)
		} else {
			columns[index] = fmt.Sprintf("%*s", t.widths[index], value)
		}
	}
	return strings.Join(columns, "  ")
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
	barHeight := min(12, max(1, height-3))
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
	var countLine strings.Builder
	for index, count := range buckets {
		countLine.WriteString(centerText(fmt.Sprintf("%d", count), barWidth))
		if index+1 < len(buckets) {
			countLine.WriteString(mutedStyle.Render("│"))
		}
	}
	lines = append(lines, centerLine(countLine.String(), width))
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
