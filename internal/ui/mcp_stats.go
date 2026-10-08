package ui

import (
	"fmt"
	"github.com/a-bali/gomodeltui/internal/gomodel"
	"github.com/a-bali/gomodeltui/internal/latency"
	"github.com/charmbracelet/lipgloss"
	"sort"
	"strings"
)

// Derive statistics from retained, deduplicated logs so backfill and lazy
// details update the same calls without maintaining a second copy of history.
func (m Model) mcpSummaries() []latency.Summary {
	byMethod := make(map[string]*latency.Summary)
	for _, request := range m.logs {
		if !request.IsMCP() || !request.Terminal {
			continue
		}
		key := request.MCPAction()
		s := byMethod[key]
		if s == nil {
			s = &latency.Summary{Key: key}
			byMethod[key] = s
		}
		s.Attempts++
		_, response := gomodel.MCPBodies(request.RawJSON)
		result, _ := response["result"].(map[string]any)
		toolError, _ := result["isError"].(bool)
		if request.Success && response["error"] == nil && !toolError {
			s.Success++
		} else {
			s.Errors++
		}
		if request.Duration > 0 {
			s.Durations = append(s.Durations, request.Duration)
		}
	}
	summaries := make([]latency.Summary, 0, len(byMethod))
	for _, s := range byMethod {
		sort.Slice(s.Durations, func(i, j int) bool { return s.Durations[i] < s.Durations[j] })
		summaries = append(summaries, *s)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Attempts != summaries[j].Attempts {
			return summaries[i].Attempts > summaries[j].Attempts
		}
		return summaries[i].Key < summaries[j].Key
	})
	return summaries
}

func (m Model) renderMCPScreen() string {
	header := lipgloss.NewStyle().Bold(true).Render("MCP methods") + mutedStyle.Render(fmt.Sprintf("  retained %s  m/Esc back  ↑↓/PgUp/PgDn scroll  Home/End", m.logRetention))
	summaries := m.mcpSummaries()
	if len(summaries) == 0 {
		return header + "\n\n" + mutedStyle.Render("No completed MCP calls observed yet.")
	}
	values := [][]string{{"method / tool", "calls", "ok", "err", "p50", "p95", "max"}}
	widths := make([]int, 7)
	for _, s := range summaries {
		timings := []string{"-", "-", "-"}
		if len(s.Durations) > 0 {
			timings = []string{formatLatency(latency.Percentile(s.Durations, 50)), formatLatency(latency.Percentile(s.Durations, 95)), formatLatency(maxDuration(s.Durations))}
		}
		values = append(values, append([]string{s.Key, fmt.Sprint(s.Attempts), fmt.Sprint(s.Success), fmt.Sprint(s.Errors)}, timings...))
	}
	for _, row := range values {
		for i, value := range row {
			widths[i] = max(widths[i], lipgloss.Width(value))
		}
	}
	used := 2 * (len(widths) - 1)
	for _, width := range widths {
		used += width
	}
	widths[0] += max(0, m.width-2-used)
	table := latencyTable{widths: widths}
	lines := []string{header, mutedStyle.Render(table.format(values[0]))}
	rows := max(1, m.height-2)
	start := min(m.mcpOffset, max(0, len(summaries)-rows))
	thumbStart, thumbEnd := scrollbarThumb(rows, len(summaries), start)
	for i := start; i < min(len(summaries), start+rows); i++ {
		lines = append(lines, table.format(values[i+1])+" "+scrollbarCell(i-start, thumbStart, thumbEnd))
	}
	return strings.Join(lines, "\n")
}
