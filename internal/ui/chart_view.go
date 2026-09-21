package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/charmbracelet/lipgloss"
)

var successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
var failoverStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
var mutedStyle = lipgloss.NewStyle().Faint(true)
var selectionStyle = lipgloss.NewStyle().Background(lipgloss.Color("237"))
var jsonKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
var jsonStringStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
var jsonNumberStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
var jsonLiteralStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
var popupSystemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
var popupDeveloperStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
var popupUserStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
var popupAssistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
var popupToolStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
var popupSectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
var popupKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
var popupValueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

const chartAxisWidth = 7

// renderChart draws a compact, btop-inspired activity trace. The left scale is
// calculated from the busiest visible bucket so both quiet and busy periods
// remain readable.
func renderChart(buckets []chart.Bucket, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	axisWidth := 0
	if width >= chartAxisWidth+1 {
		axisWidth = chartAxisWidth
	}
	graphWidth := max(1, width-axisWidth)
	if len(buckets) > graphWidth {
		buckets = buckets[len(buckets)-graphWidth:]
	}
	scale := chartScale(chart.MaxTotal(buckets))
	rows := make([]int, len(buckets))
	for index, bucket := range buckets {
		level := int(math.Round(float64(bucket.Total()) / float64(scale) * float64(max(1, height-1))))
		rows[index] = height - 1 - min(height-1, level)
	}
	ticks := map[int]int{0: scale, height - 1: 0}
	if height >= 4 {
		ticks[(height-1)/2] = scale / 2
	}
	var lines []string
	for row := 0; row < height; row++ {
		var line strings.Builder
		if axisWidth > 0 {
			if tick, ok := ticks[row]; ok {
				line.WriteString(mutedStyle.Render(fmt.Sprintf("%5d ┤", tick)))
			} else {
				line.WriteString(mutedStyle.Render("      │"))
			}
		}
		for index, bucket := range buckets {
			glyph := " "
			if _, ok := ticks[row]; ok {
				glyph = mutedStyle.Render("┈")
			}
			if rows[index] != row {
				line.WriteString(glyph)
				continue
			}
			trace := "●"
			if index > 0 {
				switch {
				case rows[index] == rows[index-1]:
					trace = "─"
				case rows[index] < rows[index-1]:
					trace = "╱"
				default:
					trace = "╲"
				}
			}
			if bucket.Errors > 0 {
				line.WriteString(errorStyle.Render(trace))
			} else {
				line.WriteString(successStyle.Render(trace))
			}
		}
		for index := len(buckets); index < graphWidth; index++ {
			if _, ok := ticks[row]; ok {
				line.WriteString(mutedStyle.Render("┈"))
			} else {
				line.WriteByte(' ')
			}
		}
		lines = append(lines, line.String())
	}
	lines = append(lines, mutedStyle.Render(strings.Repeat("─", width)))
	return strings.Join(lines, "\n")
}

func chartScale(value int) int {
	if value <= 1 {
		return 1
	}
	power := int(math.Pow10(int(math.Floor(math.Log10(float64(value))))))
	for _, multiplier := range []int{1, 2, 5, 10} {
		candidate := multiplier * power
		if value <= candidate {
			return candidate
		}
	}
	return 10 * power
}
