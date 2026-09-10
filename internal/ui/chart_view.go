package ui

import (
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

func renderChart(buckets []chart.Bucket, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	if len(buckets) > width {
		buckets = buckets[len(buckets)-width:]
	}
	maxTotal := chart.MaxTotal(buckets)
	barWidth := max(1, width/len(buckets))
	var lines []string
	for row := height; row > 0; row-- {
		var line strings.Builder
		for _, bucket := range buckets {
			green := bucket.Success * height / maxTotal
			red := bucket.Errors * height / maxTotal
			filled := green + red
			glyph := " "
			if filled >= row {
				if row <= red {
					glyph = errorStyle.Render("█")
				} else {
					glyph = successStyle.Render("█")
				}
			}
			for index := 0; index < barWidth; index++ {
				line.WriteString(glyph)
			}
		}
		lines = append(lines, line.String())
	}
	lines = append(lines, strings.Repeat("─", width))
	return strings.Join(lines, "\n")
}
