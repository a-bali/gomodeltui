package ui

import (
	"strings"
	"testing"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/charmbracelet/lipgloss"
)

func TestRenderChartScalesAndPlotsBrailleArea(t *testing.T) {
	got := renderChart([]chart.Bucket{{Success: 2, Errors: 1}}, 20, 5)
	if !strings.Contains(got, "─┤2") || !containsBraille(got) {
		t.Fatalf("expected scaled trace:\n%s", got)
	}
	if lines := strings.Count(got, "\n"); lines != 5 {
		t.Fatalf("expected 3 rows, got %d", lines)
	}
}

func TestResampleChartBucketsInterpolatesAcrossBraillePixels(t *testing.T) {
	got := resampleChartBuckets([]chart.Bucket{{Success: 0}, {Success: 4}}, 5)
	for index, want := range []int{0, 1, 2, 3, 4} {
		if got[index].Success != want {
			t.Fatalf("sample %d=%d, want %d", index, got[index].Success, want)
		}
	}
}

func containsBraille(text string) bool {
	for _, value := range text {
		if value >= 0x2800 && value <= 0x28FF {
			return true
		}
	}
	return false
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
