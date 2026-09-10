package ui

import (
	"strings"
	"testing"

	"github.com/balia/gomodeltui/internal/chart"
)

func TestRenderChartScalesAndStacks(t *testing.T) {
	got := renderChart([]chart.Bucket{{Success: 2, Errors: 1}}, 1, 3)
	plain := strings.ReplaceAll(got, "\x1b[32m", "")
	if !strings.Contains(plain, "█") {
		t.Fatal("expected bar")
	}
	if lines := strings.Count(got, "\n"); lines != 3 {
		t.Fatalf("expected 3 rows, got %d", lines)
	}
}

func TestRenderChartChangesBarWidthWithZoom(t *testing.T) {
	bucket := chart.Bucket{Success: 1}
	short := renderChart([]chart.Bucket{bucket, bucket, bucket, bucket, bucket}, 50, 3)
	long := renderChart(make([]chart.Bucket, 50), 50, 3)
	if strings.Count(short, "█") <= strings.Count(long, "█") {
		t.Fatal("short zoom should render wider bars")
	}
}
