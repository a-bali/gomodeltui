package ui

import (
	"bufio"
	"context"
	"fmt"
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
	return &Model{client: client, reducer: gomodel.NewReducer(), store: chart.NewStore(), window: chart.Window1h, logIndex: make(map[string]int), autoFollow: true}
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
				if request.Terminal {
					m.store.Add(request.TimestampOrNow(), request.Success)
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
	header := lipgloss.NewStyle().Bold(true).Render("GoModel TUI") + "  " + status + fmt.Sprintf("  window: %s", windowLabel(m.window))
	chartText := renderChart(m.store.Snapshot(time.Now(), m.window), chartWidth, chartHeight)
	logs := m.renderLogs(m.width)
	footer := "1-6 window  +/- zoom  space pause  ↑↓ scroll  g follow  c clear  r reconnect  q quit"
	return strings.Join([]string{header, chartText, "Live requests", logs, footer}, "\n")
}

func (m Model) renderLogs(width int) string {
	rows := visibleLogRows(m.height)
	start := min(m.logOffset, max(0, len(m.logs)-rows))
	end := min(len(m.logs), start+rows)
	var out []string
	for _, request := range m.logs[start:end] {
		icon := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓")
		if request.Terminal && !request.Success {
			icon = errorStyle.Render("✗")
		}
		timestamp := request.Timestamp.Format("15:04:05")
		if request.Timestamp.IsZero() {
			timestamp = "--:--:--"
		}
		responseTime := "-"
		if request.Duration > 0 {
			responseTime = fmt.Sprintf("%dms", request.Duration/time.Millisecond)
		}
		line := fmt.Sprintf("%s %s %-12s %-16s %-20s in:%d out:%d status:%s rt:%s", icon, timestamp, request.UserPath, request.Provider, request.Model, request.InputTokens, request.OutputTokens, request.StatusCode, responseTime)
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
