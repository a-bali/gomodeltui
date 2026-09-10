package ui

import (
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"strings"
	"time"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/balia/gomodeltui/internal/gomodel"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type eventMsg struct{ event gomodel.Event }
type connectMsg struct {
	response io.ReadCloser
	reader   *bufio.Reader
}
type errMsg struct{ err error }
type tickMsg time.Time

type Model struct {
	client        *gomodel.Client
	reducer       *gomodel.Reducer
	store         *chart.Store
	window        chart.Window
	logs          []gomodel.Request
	logIndex      map[string]int
	counted       map[string]bool
	stream        io.ReadCloser
	reader        *bufio.Reader
	lastEventID   string
	connected     bool
	paused        bool
	autoFollow    bool
	logOffset     int
	width, height int
	err           string
}

func NewModel(client *gomodel.Client) *Model {
	return &Model{client: client, reducer: gomodel.NewReducer(), store: chart.NewStore(), window: chart.Window1h, logIndex: make(map[string]int), counted: make(map[string]bool), autoFollow: true}
}

func (m Model) Init() tea.Cmd { return tea.Batch(connectCmd(m.client, m.lastEventID), refreshCmd()) }

func refreshCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(at time.Time) tea.Msg { return tickMsg(at) })
}

func connectCmd(client *gomodel.Client, lastID string) tea.Cmd {
	return func() tea.Msg {
		response, err := client.LiveLogs(context.Background(), lastID)
		if err != nil {
			return errMsg{err}
		}
		return connectMsg{response: response.Body, reader: bufio.NewReader(response.Body)}
	}
}

func readEventCmd(reader *bufio.Reader) tea.Cmd {
	return func() tea.Msg {
		event, err := gomodel.ReadEvent(reader)
		if err != nil {
			return errMsg{err}
		}
		return eventMsg{event: event}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.stream != nil {
				_ = m.stream.Close()
			}
			return m, tea.Quit
		case "+", "=":
			m.window = chart.NextWindow(m.window, -1)
		case "-":
			m.window = chart.NextWindow(m.window, 1)
		case "1":
			m.window = chart.Window15m
		case "2":
			m.window = chart.Window1h
		case "3":
			m.window = chart.Window3h
		case "4":
			m.window = chart.Window6h
		case "5":
			m.window = chart.Window12h
		case "6":
			m.window = chart.Window24h
		case "space":
			m.paused = !m.paused
		case "c":
			m.logs = nil
			m.logIndex = make(map[string]int)
			m.counted = make(map[string]bool)
			m.logOffset = 0
		case "up":
			if m.logOffset > 0 {
				m.logOffset--
				m.autoFollow = false
			}
		case "down":
			if m.logOffset < max(0, len(m.logs)-1) {
				m.logOffset++
				m.autoFollow = false
			}
		case "g":
			m.autoFollow = true
			m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
		case "r":
			if m.stream != nil {
				_ = m.stream.Close()
			}
			m.connected = false
			return m, connectCmd(m.client, m.lastEventID)
		}
	case connectMsg:
		m.stream, m.reader, m.connected, m.err = msg.response, msg.reader, true, ""
		return m, readEventCmd(m.reader)
	case eventMsg:
		if msg.event.ID != "" {
			m.lastEventID = msg.event.ID
		}
		if !m.paused {
			if request, err := m.reducer.Apply(msg.event); err != nil {
				m.err = err.Error()
			} else if request != nil {
				if index, ok := m.logIndex[request.ID]; ok {
					m.logs[index] = *request
				} else {
					m.logIndex[request.ID] = len(m.logs)
					m.logs = append(m.logs, *request)
				}
				terminalEvent := msg.event.Event == "audit.completed" || msg.event.Event == "audit.failed"
				if (request.Terminal || terminalEvent) && !m.counted[request.ID] {
					// Chart windows represent when the TUI observed the completed request.
					// The request timestamp is retained for the log and may lag local time.
					m.store.Add(time.Now(), request.Success)
					m.counted[request.ID] = true
				}
				if m.autoFollow {
					m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
				}
			}
		}
		return m, readEventCmd(m.reader)
	case errMsg:
		m.connected, m.err = false, msg.err.Error()
		if m.stream != nil {
			_ = m.stream.Close()
			m.stream = nil
		}
		return m, refreshCmd()
	case tickMsg:
		if m.connected {
			return m, refreshCmd()
		}
		return m, tea.Batch(connectCmd(m.client, m.lastEventID), refreshCmd())
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Starting gomodeltui…"
	}
	chartHeight := max(4, m.height/2-3)
	chartWidth := max(10, m.width-2)
	status := "● connected"
	if !m.connected {
		status = "○ disconnected: " + m.err
	}
	left := lipgloss.NewStyle().Bold(true).Render("GoModel TUI") + "  " + status + fmt.Sprintf("  window: %s", windowLabel(m.window))
	keys := mutedStyle.Render("1-6 window  +/- zoom  space pause  ↑↓ scroll  g follow  c clear  r reconnect  q quit")
	gap := lipgloss.NewStyle().Width(max(1, m.width-lipgloss.Width(left)-lipgloss.Width(keys))).Render("")
	header := left + gap + keys
	chartText := renderChart(m.store.Snapshot(time.Now(), m.window), chartWidth, chartHeight)
	buckets := m.store.Snapshot(time.Now(), m.window)
	var success, errors int
	for _, bucket := range buckets {
		success += bucket.Success
		errors += bucket.Errors
	}
	chartLegend := successStyle.Render("success") + fmt.Sprintf(" %d  ", success) + errorStyle.Render("errors") + fmt.Sprintf(" %d", errors)
	logs := m.renderLogs(m.width)
	return strings.Join([]string{header, chartLegend, chartText, "Live requests", logs}, "\n")
}

func (m Model) renderLogs(width int) string {
	rows := visibleLogRows(m.height)
	start := min(m.logOffset, max(0, len(m.logs)-rows))
	end := min(len(m.logs), start+rows)
	var out []string
	for _, request := range m.logs[start:end] {
		icon := successStyle.Render("✓")
		if request.Terminal && !request.Success {
			icon = errorStyle.Render("✗")
		}
		timestamp := request.Timestamp.Format("15:04:05")
		if request.Timestamp.IsZero() {
			timestamp = "--:--:--"
		}
		responseTime := "-"
		if request.Duration > 0 {
			responseTime = fmt.Sprintf("%.1f", float64(request.Duration)/float64(time.Millisecond))
		}
		arrow := mutedStyle.Render(" -> ")
		line := icon + " " + mutedStyle.Render(timestamp) + " " + userPathStyle(request.UserPath).Render(request.UserPath) + arrow + request.ClientModel + arrow + request.RoutedModel + " " + mutedStyle.Render("i:") + fmt.Sprintf("%d", request.InputTokens) + " " + mutedStyle.Render("o:") + fmt.Sprintf("%d", request.OutputTokens) + " " + mutedStyle.Render("c:") + fmt.Sprintf("%.0f%%", request.CacheRatio*100) + " " + statusStyle(request.StatusCode).Render(request.StatusCode) + " " + fmt.Sprintf("%s", responseTime) + mutedStyle.Render("ms")
		if request.Error != "" {
			line += " " + request.Error
		}
		if len(line) > width {
			line = line[:max(0, width)]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func userPathStyle(path string) lipgloss.Style {
	colors := []string{"39", "75", "99", "141", "171", "178", "208", "35", "44", "64"}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(path))
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colors[hash.Sum32()%uint32(len(colors))]))
}

func statusStyle(status string) lipgloss.Style {
	if len(status) == 3 && status[0] == '2' {
		return successStyle
	}
	return errorStyle
}

func visibleLogRows(height int) int     { return max(1, height/2-4) }
func windowLabel(w chart.Window) string { return (time.Duration(w)).String() }
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
