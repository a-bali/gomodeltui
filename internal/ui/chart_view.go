package ui

import (
	"strings"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/charmbracelet/lipgloss"
)

var successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
var mutedStyle = lipgloss.NewStyle().Faint(true)

func renderChart(buckets []chart.Bucket, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	if len(buckets) > width {
		buckets = buckets[len(buckets)-width:]
	}
	for len(buckets) < width {
		buckets = append([]chart.Bucket{{}}, buckets...)
	}
	max := chart.MaxTotal(buckets)
	var lines []string
	for row := height; row > 0; row-- {
		var line strings.Builder
		for _, bucket := range buckets {
			total := bucket.Total()
			green := bucket.Success * height / max
			red := bucket.Errors * height / max
			filled := green + red
			if filled >= row {
				if row <= red {
					line.WriteString(errorStyle.Render("█"))
				} else {
					line.WriteString(successStyle.Render("█"))
				}
			} else {
				line.WriteByte(' ')
			}
			_ = total
		}
		lines = append(lines, line.String())
	}
	lines = append(lines, strings.Repeat("─", width))
	return strings.Join(lines, "\n")
}
