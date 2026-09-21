package ui

import (
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

const chartAxisWidth = 3

// renderChart draws a compact btop-inspired Braille area graph. Each terminal
// cell holds two time slices and four vertical dots, making activity changes
// legible without falling back to block bars.
func renderChart(buckets []chart.Bucket, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	axisWidth := min(chartAxisWidth, max(0, width-1))
	graphWidth := max(1, width-axisWidth)
	dotWidth := graphWidth * 2
	buckets = resampleChartBuckets(buckets, dotWidth)
	scale := chartScale(chart.MaxTotal(buckets))
	graph, errors := brailleArea(buckets, graphWidth, height, scale)
	ticks := map[int]int{0: scale, height - 1: 0}
	if height >= 4 {
		ticks[(height-1)/2] = scale / 2
	}
	var lines []string
	for row := 0; row < height; row++ {
		var line strings.Builder
		for column, glyph := range graph[row] {
			if glyph == ' ' {
				line.WriteByte(' ')
			} else if errors[row][column] {
				line.WriteString(errorStyle.Render(string(glyph)))
			} else {
				line.WriteString(successStyle.Render(string(glyph)))
			}
		}
		if axisWidth > 0 {
			line.WriteString(mutedStyle.Render(chartAxis(ticks, row, axisWidth)))
		}
		lines = append(lines, line.String())
	}
	lines = append(lines, mutedStyle.Render(strings.Repeat("─", width)))
	return strings.Join(lines, "\n")
}

// chartAxis intentionally uses a fixed three-character gutter: tick, axis,
// and a numeric scale step. The exact dynamic maximum sits in the chart legend.
func chartAxis(ticks map[int]int, row, width int) string {
	if width < chartAxisWidth {
		return strings.Repeat(" ", width)
	}
	if _, ok := ticks[row]; ok {
		step := "1"
		if row == 0 {
			step = "2"
		} else if ticks[row] == 0 {
			step = "0"
		}
		return "─┤" + step
	}
	return " │ "
}

func resampleChartBuckets(buckets []chart.Bucket, count int) []chart.Bucket {
	if count < 1 {
		return nil
	}
	if len(buckets) == 0 {
		return make([]chart.Bucket, count)
	}
	if len(buckets) == count {
		return buckets
	}
	result := make([]chart.Bucket, count)
	if len(buckets) == 1 {
		for index := range result {
			result[index] = buckets[0]
		}
		return result
	}
	for index := range result {
		position := float64(index) * float64(len(buckets)-1) / float64(count-1)
		left := int(math.Floor(position))
		right := min(len(buckets)-1, left+1)
		fraction := position - float64(left)
		result[index] = chart.Bucket{
			Success: int(math.Round(float64(buckets[left].Success)*(1-fraction) + float64(buckets[right].Success)*fraction)),
			Errors:  int(math.Round(float64(buckets[left].Errors)*(1-fraction) + float64(buckets[right].Errors)*fraction)),
		}
	}
	return result
}

func brailleArea(buckets []chart.Bucket, width, height, scale int) ([][]rune, [][]bool) {
	result := make([][]rune, height)
	errors := make([][]bool, height)
	bits := make([][]uint8, height)
	for row := range result {
		result[row] = make([]rune, width)
		errors[row] = make([]bool, width)
		bits[row] = make([]uint8, width)
	}
	brailleBits := [2][4]uint8{{1, 2, 4, 64}, {8, 16, 32, 128}}
	pixelHeight := height * 4
	for index, bucket := range buckets {
		column := index / 2
		if column >= width {
			break
		}
		side := index % 2
		filled := int(math.Round(float64(bucket.Total()) / float64(scale) * float64(pixelHeight)))
		filled = min(pixelHeight, filled)
		for pixel := 0; pixel < filled; pixel++ {
			fromTop := pixelHeight - 1 - pixel
			row, dot := fromTop/4, fromTop%4
			bits[row][column] |= brailleBits[side][dot]
			if bucket.Errors > 0 {
				errors[row][column] = true
			}
		}
	}
	for row := range result {
		for column, value := range bits[row] {
			if value != 0 {
				result[row][column] = rune(0x2800) + rune(value)
			} else {
				result[row][column] = ' '
			}
		}
	}
	return result, errors
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
