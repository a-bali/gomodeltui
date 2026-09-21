package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/charmbracelet/lipgloss"
)

func TestRenderChartScalesAndPlotsBrailleArea(t *testing.T) {
	got := renderChart([]chart.Bucket{{Success: 2, Errors: 1}}, 20, 5)
	if !strings.Contains(got, "─┤5") || !containsBraille(got) {
		t.Fatalf("expected scaled trace:\n%s", got)
	}
	if lines := strings.Count(got, "\n"); lines != 5 {
		t.Fatalf("expected 3 rows, got %d", lines)
	}
}

func TestChartAxisExpandsForActualRequestValues(t *testing.T) {
	if got := chartAxisWidth(50); got != 4 {
		t.Fatalf("axis width=%d, want 4", got)
	}
	if got := chartAxis(map[int]int{0: 50}, 0, 4); got != "─┤50" {
		t.Fatalf("axis=%q", got)
	}
}

func TestFormatChartBin(t *testing.T) {
	for duration, want := range map[time.Duration]string{12 * time.Second: "12s", 5 * time.Minute: "5m", 2 * time.Hour: "2h"} {
		if got := formatChartBin(duration); got != want {
			t.Fatalf("formatChartBin(%s)=%q, want %q", duration, got, want)
		}
	}
}

func TestRenderChartXAxisShowsStartMidpointAndNow(t *testing.T) {
	got := renderChartXAxis(30, 4, 6*time.Hour)
	for _, want := range []string{"-6h", "-3h", "now"} {
		if !strings.Contains(got, want) {
			t.Fatalf("x axis %q missing %q", got, want)
		}
	}
}

func TestExpandChartBucketsUsesBothBrailleColumnsForOneTimeBin(t *testing.T) {
	got := expandChartBuckets([]chart.Bucket{{Success: 3}})
	if len(got) != 2 || got[0].Success != 3 || got[1].Success != 3 {
		t.Fatalf("expanded buckets=%+v", got)
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
