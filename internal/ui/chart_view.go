package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/a-bali/gomodeltui/internal/chart"
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

const chartAxisMinimumWidth = 3

// renderChart draws separate stacked bars: successful attempts below failures.
// Half-block cells preserve both outcomes at the boundary between the stacks.
func renderChart(buckets []chart.Bucket, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	scale := chartScale(chart.MaxTotal(buckets))
	axisWidth := min(chartAxisWidth(scale), max(0, width-1))
	graphWidth := max(1, width-axisWidth)
	columns := (graphWidth + 1) / 2
	if len(buckets) > columns {
		buckets = buckets[len(buckets)-columns:]
	}
	ticks := map[int]int{0: scale, height - 1: 0}
	if height >= 4 {
		ticks[(height-1)/2] = scale / 2
	}
	var lines []string
	for row := 0; row < height; row++ {
		var line strings.Builder
		for column := 0; column < graphWidth; column++ {
			if column%2 != 0 || column/2 >= len(buckets) {
				line.WriteByte(' ')
				continue
			}
			line.WriteString(stackedBarCell(buckets[column/2], row, height, scale))
		}
		if axisWidth > 0 {
			line.WriteString(mutedStyle.Render(chartAxis(ticks, row, axisWidth)))
		}
		lines = append(lines, line.String())
	}
	lines = append(lines, mutedStyle.Render(strings.Repeat("─", width)))
	return strings.Join(lines, "\n")
}

func stackedBarCell(bucket chart.Bucket, row, height, scale int) string {
	if bucket.Total() == 0 {
		return " "
	}
	units := height * 2
	total := min(units, max(1, int(math.Round(float64(bucket.Total())/float64(scale)*float64(units)))))
	failed := 0
	if bucket.Errors > 0 {
		failed = max(1, int(math.Round(float64(bucket.Errors)/float64(scale)*float64(units))))
	}
	if bucket.Success > 0 && failed > 0 {
		total = min(units, max(2, total))
		failed = min(failed, total-1)
	}
	failed = min(total, failed)
	success := total - failed
	colorAt := func(unit int) int {
		if unit < success {
			return 1
		}
		if unit < total {
			return 2
		}
		return 0
	}
	lower := colorAt((height - 1 - row) * 2)
	upper := colorAt((height-1-row)*2 + 1)
	if upper == 0 && lower == 0 {
		return " "
	}
	style := successStyle
	if lower == 2 || upper == 2 {
		style = errorStyle
	}
	if upper == lower {
		return style.Render("█")
	}
	if upper == 0 {
		return style.Render("▄")
	}
	return errorStyle.Background(lipgloss.Color("42")).Render("▀")
}

func chartAxisWidth(scale int) int {
	return max(chartAxisMinimumWidth, 2+len(strconv.Itoa(scale)))
}

// chartAxis uses a tick, an axis, and an actual request-count value.
func chartAxis(ticks map[int]int, row, width int) string {
	if width < chartAxisMinimumWidth {
		return strings.Repeat(" ", width)
	}
	if value, ok := ticks[row]; ok {
		return fmt.Sprintf("─┤%*d", width-2, value)
	}
	return " │" + strings.Repeat(" ", width-2)
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

func formatChartBin(duration time.Duration) string {
	if duration >= time.Hour {
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	}
	if duration >= time.Minute {
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	}
	return fmt.Sprintf("%ds", max(1, int(duration/time.Second)))
}

func renderChartXAxis(width, axisWidth int, window time.Duration) string {
	graphWidth := max(1, width-axisWidth)
	line := make([]rune, graphWidth)
	for index := range line {
		line[index] = ' '
	}
	labels := []struct {
		text  string
		start int
	}{
		{formatChartOffset(window), 0},
		{formatChartOffset(window / 2), max(0, (graphWidth-len(formatChartOffset(window/2)))/2)},
		{"now", max(0, graphWidth-len("now"))},
	}
	for _, label := range labels {
		if label.start+len(label.text) > graphWidth {
			continue
		}
		for index, char := range label.text {
			if line[label.start+index] == ' ' {
				line[label.start+index] = char
			}
		}
	}
	return mutedStyle.Render(string(line) + strings.Repeat(" ", max(0, axisWidth)))
}

func formatChartOffset(duration time.Duration) string {
	if duration >= time.Hour {
		return fmt.Sprintf("-%dh", int(duration/time.Hour))
	}
	if duration >= time.Minute {
		return fmt.Sprintf("-%dm", int(duration/time.Minute))
	}
	return fmt.Sprintf("-%ds", max(1, int(duration/time.Second)))
}
