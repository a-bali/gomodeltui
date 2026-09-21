package ui

import (
	"strings"
	"testing"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/charmbracelet/lipgloss"
)

func TestRenderChartScalesAndPlotsTrace(t *testing.T) {
	got := renderChart([]chart.Bucket{{Success: 2, Errors: 1}}, 20, 5)
	if !strings.Contains(got, "5 ┤") || !strings.Contains(got, "●") {
		t.Fatalf("expected scaled trace:\n%s", got)
	}
	if lines := strings.Count(got, "\n"); lines != 5 {
		t.Fatalf("expected 3 rows, got %d", lines)
	}
}

func TestChartScaleRoundsUp(t *testing.T) {
	for value, want := range map[int]int{1: 1, 2: 2, 3: 5, 11: 20, 51: 100} {
		if got := chartScale(value); got != want {
			t.Fatalf("chartScale(%d)=%d, want %d", value, got, want)
		}
	}
}

func TestRenderChartDividerUsesFullWidth(t *testing.T) {
	const width = 79
	lines := strings.Split(renderChart([]chart.Bucket{{Success: 1}}, width, 3), "\n")
	divider := lines[len(lines)-1]
	if got := lipgloss.Width(divider); got != width {
		t.Fatalf("divider width=%d, want %d", got, width)
	}
}
