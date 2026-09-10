package ui

import (
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/balia/gomodeltui/internal/gomodel"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type eventMsg struct{ event gomodel.Event }
type connectMsg struct {
	response io.ReadCloser
	reader   *bufio.Reader
}
type errMsg struct{ err error }
type tickMsg time.Time

const maxLogItems = 1000

type Model struct {
	client        *gomodel.Client
	reducer       *gomodel.Reducer
	store         *chart.Store
	window        chart.Window
	logs          []gomodel.Request
	logIndex      map[string]int
	counted       map[string]bool
	selected      int
	following     bool
	popup         bool
	popupLines    []string
	popupRawLines []string
	popupRaw      bool
	popupMessages []popupMessage
	popupMessage  int
	popupAll      bool
	popupOffset   int
	searching     bool
	searchQuery   string
	stream        io.ReadCloser
	reader        *bufio.Reader
	lastEventID   string
	connected     bool
	logOffset     int
	width, height int
	err           string
}

func NewModel(client *gomodel.Client) *Model {
	return &Model{client: client, reducer: gomodel.NewReducer(), store: chart.NewStore(), window: chart.Window1h, logIndex: make(map[string]int), counted: make(map[string]bool), following: true}
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
		if m.popup {
			switch msg.String() {
			case "esc":
				m.popup = false
			case "tab":
				// Structured and raw JSON are intentionally the only two views.
			case "r":
				m.popupRaw = !m.popupRaw
				m.popupOffset = 0
			case "enter":
				if !m.popupRaw && len(m.popupMessages) > 0 {
					m.popupMessages[m.popupMessage].expanded = !m.popupMessages[m.popupMessage].expanded
					m.ensurePopupMessageVisible()
				}
			case "left":
				if !m.popupRaw && len(m.popupMessages) > 0 {
					m.popupMessages[m.popupMessage].expanded = false
					m.ensurePopupMessageVisible()
				}
			case "right":
				if !m.popupRaw && len(m.popupMessages) > 0 {
					m.popupMessages[m.popupMessage].expanded = true
					m.ensurePopupMessageVisible()
				}
			case "space", " ":
				if !m.popupRaw {
					m.popupAll = !m.popupAll
					for index := range m.popupMessages {
						m.popupMessages[index].expanded = m.popupAll
					}
					m.popupOffset = 0
				}
			case "up":
				if !m.popupRaw && m.popupMessage > 0 {
					m.popupMessage--
					m.ensurePopupMessageVisible()
				} else if m.popupOffset > 0 {
					m.popupOffset--
				}
			case "down":
				if !m.popupRaw && m.popupMessage+1 < len(m.popupMessages) {
					m.popupMessage++
					m.ensurePopupMessageVisible()
				} else if m.popupOffset < max(0, len(m.popupContentLines())-popupRows(m.height)) {
					m.popupOffset++
				}
			case "pgup", "pageup":
				m.popupOffset = max(0, m.popupOffset-popupRows(m.height))
			case "pgdown", "pagedown":
				m.popupOffset = min(max(0, len(m.popupContentLines())-popupRows(m.height)), m.popupOffset+popupRows(m.height))
			case "home", "ctrl+home":
				m.popupOffset = 0
				m.popupMessage = 0
			case "end", "ctrl+end":
				if !m.popupRaw && len(m.popupMessages) > 0 {
					m.popupMessage = len(m.popupMessages) - 1
				}
				m.popupOffset = max(0, len(m.popupContentLines())-popupRows(m.height))
			}
			return m, nil
		}
		if m.searching {
			switch msg.String() {
			case "enter":
				m.searching = false
				m.findNextMatch()
			case "esc":
				m.searching = false
			case "backspace", "ctrl+h":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				}
			case "ctrl+c":
				m.searching = false
			default:
				if msg.Type == tea.KeyRunes {
					m.searchQuery += string(msg.Runes)
				}
			}
			return m, nil
		}
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
			m.window = chart.Window5m
		case "2":
			m.window = chart.Window15m
		case "3":
			m.window = chart.Window1h
		case "4":
			m.window = chart.Window3h
		case "5":
			m.window = chart.Window6h
		case "6":
			m.window = chart.Window12h
		case "7":
			m.window = chart.Window24h
		case "space", " ":
			m.following = !m.following
			if m.following {
				m.selected = max(0, len(m.logs)-1)
				m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
			}
		case "c":
			m.logs = nil
			m.logIndex = make(map[string]int)
			m.counted = make(map[string]bool)
			m.selected = 0
			m.logOffset = 0
		case "up":
			m.moveSelection(-1)
		case "down":
			m.moveSelection(1)
		case "pgup", "pageup":
			m.moveSelection(-visibleLogRows(m.height))
		case "pgdown", "pagedown":
			m.moveSelection(visibleLogRows(m.height))
		case "home":
			m.following = false
			m.selected = 0
			m.logOffset = 0
		case "end":
			m.following = false
			m.selected = max(0, len(m.logs)-1)
			m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
		case "g":
			m.following = true
			m.selected = max(0, len(m.logs)-1)
			m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
		case "/":
			m.searching = true
			m.following = false
		case "n":
			if m.searchQuery != "" {
				m.findNextMatch()
			}
		case "enter":
			if len(m.logs) > 0 && m.selected < len(m.logs) {
				m.popup = true
				m.popupOffset = 0
				m.popupRaw = false
				m.popupMessage = 0
				m.popupAll = false
				m.popupRawLines = strings.Split(m.logs[m.selected].RawJSON, "\n")
				m.popupLines = buildPopupSummaryLines(m.logs[m.selected].RawJSON)
				m.popupMessages = parsePopupMessages(m.logs[m.selected].RawJSON)
				if len(m.popupMessages) > 0 {
					m.popupMessage = len(m.popupMessages) - 1
					if !popupHasResponse(m.logs[m.selected].RawJSON) {
						m.popupOffset = max(0, len(m.popupContentLines())-popupRows(m.height))
					}
				}
			}
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
		{
			if request, err := m.reducer.Apply(msg.event); err != nil {
				m.err = err.Error()
			} else if request != nil {
				rows := request.LogRows()
				m.replaceRequestRows(request.ID, rows)
				terminalEvent := msg.event.Event == "audit.completed" || msg.event.Event == "audit.failed"
				if (request.Terminal || terminalEvent) && !m.counted[request.ID] {
					// Chart windows represent when the TUI observed the completed request.
					// The request timestamp is retained for the log and may lag local time.
					for _, row := range rows {
						m.store.Add(time.Now(), row.Success)
					}
					m.counted[request.ID] = true
				}
				if m.following {
					m.selected = max(0, len(m.logs)-1)
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
	chartHeight := chartAreaHeight(m.height)
	chartWidth := max(10, m.width-2)
	dot := errorStyle.Render("●")
	if m.connected {
		dot = successStyle.Render("●")
	}
	left := dot + " " + lipgloss.NewStyle().Bold(true).Render("GoModel TUI") + fmt.Sprintf("  window: %s", windowLabel(m.window))
	followLabel := "follow:on"
	if !m.following {
		followLabel = "follow:off"
	}
	keysText := "1-7 window  +/- zoom  space " + followLabel + "  ↑↓/PgUp/PgDn select  Enter JSON  / search  n next  q quit"
	if m.searching {
		keysText = "/" + m.searchQuery + "  Enter find  Esc cancel"
	}
	keys := mutedStyle.Render(keysText)
	gap := lipgloss.NewStyle().Width(max(1, m.width-lipgloss.Width(left)-lipgloss.Width(keys))).Render("")
	header := left + gap + keys
	buckets := m.store.Snapshot(time.Now(), m.window, chartWidth)
	chartText := renderChart(buckets, chartWidth, chartHeight)
	var success, errors int
	for _, bucket := range buckets {
		success += bucket.Success
		errors += bucket.Errors
	}
	chartLegend := successStyle.Render("success") + fmt.Sprintf(" %d  ", success) + errorStyle.Render("errors") + fmt.Sprintf(" %d", errors)
	logs := m.renderLogs(m.width)
	if m.popup {
		return m.renderPopup()
	}
	return strings.Join([]string{header, chartLegend, chartText, logs}, "\n")
}

func (m Model) renderLogs(width int) string {
	rows := visibleLogRows(m.height)
	start := min(m.logOffset, max(0, len(m.logs)-rows))
	end := min(len(m.logs), start+rows)
	contentWidth := max(1, width-2)
	thumbStart, thumbEnd := scrollbarThumb(rows, len(m.logs), start)
	var out []string
	for index, request := range m.logs[start:end] {
		icon := successStyle.Render("✓")
		if request.Terminal && !request.Success {
			icon = errorStyle.Render("✗")
		} else if request.Terminal && request.Failover {
			icon = failoverStyle.Render("✓")
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
		session := ""
		if len(request.SessionID) > 0 {
			session = sessionStyle(request.SessionID).Render("sid:" + request.SessionID[max(0, len(request.SessionID)-3):])
		}
		route := request.RoutedModel
		if request.Failover {
			route += " (failover)"
		}
		prefix := icon + " " + mutedStyle.Render(timestamp) + " " + userPathStyle(request.UserPath).Render(request.UserPath) + " " + session + arrow + request.ClientModel + arrow + route + " " + mutedStyle.Render("i:") + fmt.Sprintf("%d", request.InputTokens) + " " + mutedStyle.Render("o:") + fmt.Sprintf("%d", request.OutputTokens) + " " + mutedStyle.Render("c:") + fmt.Sprintf("%.0f%%", request.CacheRatio*100) + " " + statusStyle(request.StatusCode).Render(request.StatusCode) + " " + responseTime + mutedStyle.Render("ms")
		if request.Error != "" {
			prefix += " " + request.Error
		}
		line := prefix
		if request.LastTurn != "" {
			separator := mutedStyle.Render("  ")
			available := max(0, contentWidth-lipgloss.Width(prefix)-lipgloss.Width(separator))
			line = prefix + separator + mutedStyle.Render(truncateText(collapsePreview(request.LastTurn), available))
		}
		line = ansi.Truncate(line, contentWidth, "…")
		line += strings.Repeat(" ", max(0, contentWidth-lipgloss.Width(line)))
		if start+len(out) == m.selected {
			line = renderSelectedLine(line)
		}
		line += " " + scrollbarCell(index, thumbStart, thumbEnd)
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func renderSelectedLine(line string) string {
	const background = "\x1b[48;5;237m"
	rendered := selectionStyle.Render(line)
	for _, reset := range []string{"\x1b[0m", "\x1b[39m"} {
		rendered = strings.ReplaceAll(rendered, reset, reset+background)
	}
	return strings.TrimSuffix(rendered, background)
}

func scrollbarThumb(rows, total, offset int) (int, int) {
	if rows <= 0 || total <= rows {
		return 0, rows
	}
	thumbSize := max(1, rows*rows/total)
	track := rows - thumbSize
	position := 0
	if total > rows {
		position = offset * track / (total - rows)
	}
	return position, position + thumbSize
}

func scrollbarCell(row, thumbStart, thumbEnd int) string {
	if row >= thumbStart && row < thumbEnd {
		return mutedStyle.Render("█")
	}
	return mutedStyle.Render("│")
}

func (m *Model) moveSelection(delta int) {
	if len(m.logs) == 0 {
		return
	}
	m.following = false
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.logs) {
		m.selected = len(m.logs) - 1
	}
	rows := visibleLogRows(m.height)
	if m.selected < m.logOffset {
		m.logOffset = m.selected
	}
	if m.selected >= m.logOffset+rows {
		m.logOffset = m.selected - rows + 1
	}
}

func popupRows(height int) int { return max(1, height-1) }

func (m Model) renderPopup() string {
	lines := m.popupContentLines()
	rows := popupRows(m.height)
	start := min(m.popupOffset, max(0, len(lines)-rows))
	end := min(len(lines), start+rows)
	thumbStart, thumbEnd := scrollbarThumb(rows, len(lines), start)
	header := "Request structured  (r raw JSON  Enter expand/collapse  Space all  Esc close)"
	if m.popupRaw {
		header = "Raw JSON  (r structured view  Home/End  ↑↓/PgUp/PgDn scroll  Esc close)"
	}
	content := []string{truncateText(header, max(1, m.width))}
	for _, line := range lines[start:end] {
		if m.popupRaw {
			line = highlightJSONLine(line)
		} else {
			line = styleStructuredLine(line)
		}
		line += strings.Repeat(" ", max(0, m.width-2-lipgloss.Width(line)))
		line += " " + scrollbarCell(len(content)-1, thumbStart, thumbEnd)
		content = append(content, line)
	}
	return strings.Join(content, "\n")
}

func (m Model) popupContentLines() []string {
	lines := m.popupLines
	if !m.popupRaw && len(m.popupMessages) > 0 {
		lines = append(append([]string{}, m.popupLines...), m.renderPopupMessages()...)
	}
	if m.popupRaw && len(m.popupRawLines) > 0 {
		lines = m.popupRawLines
	}
	return wrapJSONLines(lines, max(1, m.width-2))
}

func (m *Model) ensurePopupMessageVisible() {
	if m.popupRaw || len(m.popupMessages) == 0 {
		return
	}
	lines := m.popupContentLines()
	rows := popupRows(m.height)
	header := len(m.popupLines)
	for index := 0; index < m.popupMessage; index++ {
		header += len(m.renderOnePopupMessage(index))
	}
	if header < m.popupOffset {
		m.popupOffset = header
	} else if header >= m.popupOffset+rows {
		m.popupOffset = header - rows + 1
	}
	if len(lines) == 0 {
		m.popupOffset = 0
	}
}

func (m Model) renderPopupMessages() []string {
	var lines []string
	for index := range m.popupMessages {
		lines = append(lines, m.renderOnePopupMessage(index)...)
	}
	return lines
}

func (m Model) renderOnePopupMessage(index int) []string {
	message := m.popupMessages[index]
	var lines []string
	marker := "  "
	if index == m.popupMessage {
		marker = "> "
	}
	role := popupRoleStyle(message.role).Render(strings.ToUpper(message.role))
	state := "▶"
	if message.expanded {
		state = "▼"
	}
	lines = append(lines, fmt.Sprintf("%s%s [%d] %s", marker, state, index+1, role))
	if message.expanded {
		for _, line := range message.lines {
			lines = append(lines, "    "+line)
		}
	} else {
		lines = append(lines, "    "+mutedStyle.Render("(press Enter to expand)"))
	}
	return lines
}

func styleStructuredLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "REQUEST" || strings.HasPrefix(trimmed, "ROUTING ATTEMPTS") || strings.HasPrefix(trimmed, "RESPONSE") || strings.HasPrefix(trimmed, "MESSAGES") {
		return popupSectionStyle.Render(line)
	}
	if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "▶") || strings.HasPrefix(trimmed, "▼") || strings.HasPrefix(trimmed, "...") {
		return line
	}
	if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
		body := strings.TrimLeft(line, " ")
		if keyEnd := strings.IndexAny(body, " \t"); keyEnd > 0 {
			indent := line[:len(line)-len(body)]
			key := body[:keyEnd]
			value := strings.TrimLeft(body[keyEnd:], " \t")
			return indent + popupKeyStyle.Render(key) + " " + popupValueStyle.Render(value)
		}
	}
	return line
}

func wrapJSONLines(lines []string, width int) []string {
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, wrapJSONLine(line, width)...)
	}
	return wrapped
}

func wrapJSONLine(line string, width int) []string {
	if width < 1 {
		width = 1
	}
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	var wrapped []string
	for len(runes) > width {
		cut := width
		for index := width; index > 0; index-- {
			if unicode.IsSpace(runes[index-1]) {
				cut = index
				break
			}
		}
		if cut == 0 {
			cut = width
		}
		part := string(runes[:cut])
		wrapped = append(wrapped, part)
		runes = runes[cut:]
	}
	wrapped = append(wrapped, string(runes))
	return wrapped
}

func highlightJSONLine(line string) string {
	var out strings.Builder
	for index := 0; index < len(line); {
		switch line[index] {
		case ' ', '\t':
			out.WriteByte(line[index])
			index++
		case '"':
			end := index + 1
			for end < len(line) {
				if line[end] == '"' && line[end-1] != '\\' {
					end++
					break
				}
				end++
			}
			style := jsonStringStyle
			lookahead := end
			for lookahead < len(line) && (line[lookahead] == ' ' || line[lookahead] == '\t') {
				lookahead++
			}
			if lookahead < len(line) && line[lookahead] == ':' {
				style = jsonKeyStyle
			}
			out.WriteString(style.Render(line[index:end]))
			index = end
		case '{', '}', '[', ']', ':', ',':
			out.WriteString(mutedStyle.Render(string(line[index])))
			index++
		default:
			end := index
			for end < len(line) && !strings.ContainsRune(" \t{}[]:,", rune(line[end])) {
				end++
			}
			token := line[index:end]
			style := jsonNumberStyle
			if token == "true" || token == "false" || token == "null" {
				style = jsonLiteralStyle
			}
			out.WriteString(style.Render(token))
			index = end
		}
	}
	return out.String()
}

func (m *Model) replaceRequestRows(logicalID string, rows []gomodel.Request) {
	start := len(m.logs)
	filtered := make([]gomodel.Request, 0, len(m.logs)+len(rows))
	for _, row := range m.logs {
		if row.ID == logicalID || strings.HasPrefix(row.ID, logicalID+"/attempt-") {
			if start == len(m.logs) {
				start = len(filtered)
			}
			continue
		}
		filtered = append(filtered, row)
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	filtered = append(filtered[:start], append(rows, filtered[start:]...)...)
	m.logs = filtered
	if excess := len(m.logs) - maxLogItems; excess > 0 {
		m.logs = m.logs[excess:]
		m.selected = max(0, m.selected-excess)
		m.logOffset = max(0, m.logOffset-excess)
	}
	m.logIndex = make(map[string]int)
	for index, row := range m.logs {
		if !strings.Contains(row.ID, "/attempt-") {
			m.logIndex[row.ID] = index
		}
	}
}

func (m *Model) findNextMatch() {
	if len(m.logs) == 0 || m.searchQuery == "" {
		return
	}
	query := strings.ToLower(m.searchQuery)
	for step := 1; step <= len(m.logs); step++ {
		index := (m.selected + step) % len(m.logs)
		if strings.Contains(strings.ToLower(requestSearchText(m.logs[index])), query) {
			m.selected = index
			m.following = false
			rows := visibleLogRows(m.height)
			if m.selected < m.logOffset {
				m.logOffset = m.selected
			} else if m.selected >= m.logOffset+rows {
				m.logOffset = m.selected - rows + 1
			}
			return
		}
	}
}

func requestSearchText(request gomodel.Request) string {
	return strings.Join([]string{
		request.UserPath, request.SessionID, request.ClientModel, request.RoutedModel,
		request.Model, request.Provider, request.StatusCode, request.Error, request.LastTurn,
		request.RawJSON,
	}, " ")
}

func userPathStyle(path string) lipgloss.Style {
	colors := []string{"39", "75", "99", "141", "171", "178", "208", "35", "44", "64"}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(path))
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colors[hash.Sum32()%uint32(len(colors))]))
}

func sessionStyle(id string) lipgloss.Style { return userPathStyle("session:" + id) }

func truncateText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func collapsePreview(text string) string { return strings.Join(strings.Fields(text), " ") }

func statusStyle(status string) lipgloss.Style {
	if len(status) == 3 && status[0] == '2' {
		return successStyle
	}
	return errorStyle
}

func chartAreaHeight(height int) int    { return max(4, height/3-2) }
func visibleLogRows(height int) int     { return max(1, height-chartAreaHeight(height)-4) }
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
