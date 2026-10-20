package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/a-bali/gomodeltui/internal/chart"
	"github.com/a-bali/gomodeltui/internal/gomodel"
	"github.com/a-bali/gomodeltui/internal/latency"
	"github.com/a-bali/gomodeltui/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type eventMsg struct {
	event        gomodel.Event
	connectionID uint64
}
type connectMsg struct {
	response     io.ReadCloser
	reader       *bufio.Reader
	connectionID uint64
}
type errMsg struct {
	err          error
	connectionID uint64
}
type tickMsg time.Time
type liveLatencyTickMsg time.Time
type usageMsg struct {
	provider string
	snapshot usage.Snapshot
	err      error
	request  uint64
}
type backfillMsg struct {
	events     []gomodel.Event
	err        error
	start      time.Time
	generation uint64
}
type auditDetailMsg struct {
	generation uint64
	requestID  string
	event      gomodel.Event
	err        error
}

const (
	reconnectInterval = 10 * time.Second
	historyRetention  = 24 * time.Hour
)

type Model struct {
	client              *gomodel.Client
	reducer             *gomodel.Reducer
	store               *chart.Store
	mcpStore            *chart.Store
	includeMCP          bool
	allLogs             []gomodel.Request
	latencyStore        *latency.Store
	window              chart.Window
	logs                []gomodel.Request
	logIndex            map[string]int
	counted             map[string]time.Time
	historyCoveredFrom  time.Time
	backfillPendingFrom time.Time
	backfillGeneration  uint64
	selected            int
	following           bool
	followPulse         uint8
	liveLatencyTicker   bool
	latencyScreen       bool
	latencySelected     int
	latencyOffset       int
	latencyScaleMax     time.Duration
	latencyScaleAt      time.Time
	latencyBuckets      int
	latencyMode         latencyDistributionMode
	usageScreen         bool
	mcpScreen           bool
	mcpOffset           int
	usageFetchers       []usage.Fetcher
	usageSnapshots      map[string]usage.Snapshot
	usageErrors         map[string]string
	usageRefreshAt      time.Time
	usageRequest        uint64
	usagePending        int
	popup               bool
	popupLines          []string
	popupRawLines       []string
	popupRaw            bool
	popupMessages       []popupMessage
	popupMessage        int
	popupAll            bool
	popupOffset         int
	popupRequestID      string
	popupAuditLogID     string
	popupLoading        bool
	popupLoadError      string
	popupRecord         gomodel.Request
	popupGeneration     uint64
	popupCancel         context.CancelFunc
	previewPending      map[string]previewWork
	previewDone         map[string]bool
	previewRetry        map[string]time.Time
	previewSequence     uint64
	stopping            bool
	searching           bool
	searchQuery         string
	stream              io.ReadCloser
	reader              *bufio.Reader
	lastEventID         string
	connected           bool
	connecting          bool
	lastConnectAt       time.Time
	connectionID        uint64
	logOffset           int
	width, height       int
	err                 string
}

func NewModel(client *gomodel.Client, usageFetchers ...usage.Fetcher) *Model {
	return &Model{client: client, reducer: gomodel.NewReducer(), store: chart.NewStore(), latencyStore: latency.NewStore(), window: chart.Window1h, latencyBuckets: latencyHistogramBuckets, usageFetchers: usageFetchers, usageSnapshots: make(map[string]usage.Snapshot), usageErrors: make(map[string]string), logIndex: make(map[string]int), counted: make(map[string]time.Time), following: true, connecting: true, lastConnectAt: time.Now(), connectionID: 1, backfillGeneration: 1}
}

func (m Model) Init() tea.Cmd {
	if m.client == nil {
		return refreshCmd()
	}
	return tea.Batch(connectCmd(m.client, m.lastEventID, m.connectionID), backfillCmd(m.client, time.Now().Add(-time.Duration(m.window)), m.backfillGeneration), refreshCmd())
}

func refreshCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(at time.Time) tea.Msg { return tickMsg(at) })
}

func liveLatencyRefreshCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(at time.Time) tea.Msg { return liveLatencyTickMsg(at) })
}

func connectCmd(client *gomodel.Client, lastID string, connectionID uint64) tea.Cmd {
	return func() tea.Msg {
		// The SSE response body must retain its request context for the entire
		// lifetime of the stream. A timeout context canceled after headers arrive
		// would make a healthy stream look disconnected while buffered events can
		// still be drained from it.
		response, err := client.LiveLogs(context.Background(), lastID)
		if err != nil {
			return errMsg{err: err, connectionID: connectionID}
		}
		return connectMsg{response: response.Body, reader: bufio.NewReader(response.Body), connectionID: connectionID}
	}
}

func backfillCmd(client *gomodel.Client, start time.Time, generation uint64) tea.Cmd {
	return func() tea.Msg {
		events, err := client.AuditLogs(context.Background(), start)
		return backfillMsg{events: events, err: err, start: start, generation: generation}
	}
}

func (m *Model) requestWindowBackfill(force ...bool) tea.Cmd {
	if m.client == nil {
		return nil
	}
	start := time.Now().Add(-time.Duration(m.window))
	forced := len(force) > 0 && force[0]
	if !forced && !m.historyCoveredFrom.IsZero() && !start.Before(m.historyCoveredFrom) {
		return nil
	}
	if !m.backfillPendingFrom.IsZero() && !start.Before(m.backfillPendingFrom) {
		return nil
	}
	m.backfillGeneration++
	m.backfillPendingFrom = start
	return backfillCmd(m.client, start, m.backfillGeneration)
}

func auditDetailCmd(ctx context.Context, client *gomodel.Client, auditLogID, requestID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		event, err := client.RequestDetail(ctx, auditLogID, requestID)
		return auditDetailMsg{requestID: requestID, event: event, err: err, generation: generation}
	}
}

func readEventCmd(reader *bufio.Reader, connectionID uint64) tea.Cmd {
	return func() tea.Msg {
		event, err := gomodel.ReadEvent(reader)
		if err != nil {
			return errMsg{err: err, connectionID: connectionID}
		}
		return eventMsg{event: event, connectionID: connectionID}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, command := m.updateState(msg)
	return model, tea.Batch(command, m.ensureLiveLatencyTicker(), m.schedulePreviews())
}

func (m *Model) ensureLiveLatencyTicker() tea.Cmd {
	if m.liveLatencyTicker || !m.hasVisiblePendingRequest() {
		return nil
	}
	m.liveLatencyTicker = true
	return liveLatencyRefreshCmd()
}

func (m *Model) hasVisiblePendingRequest() bool {
	if m.popup || m.latencyScreen || m.mcpScreen || m.usageScreen || len(m.logs) == 0 {
		return false
	}
	rows := visibleLogRows(m.height)
	start := min(max(m.logOffset, 0), len(m.logs))
	end := min(len(m.logs), start+rows)
	for _, request := range m.logs[start:end] {
		if !request.Terminal {
			return true
		}
	}
	return false
}

func (m *Model) updateState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.stopContentWork()
			if m.stream != nil {
				_ = m.stream.Close()
			}
			return m, tea.Quit
		}
		if m.popup {
			switch msg.String() {
			case "esc":
				m.popup = false
				m.cancelPopupDetail()
				m.popupRawLines, m.popupLines, m.popupMessages = nil, nil, nil
				m.popupRecord = gomodel.Request{}
			case "R":
				return m, m.loadPopupDetail()
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
		if m.mcpScreen {
			rows := max(1, m.height-2)
			last := max(0, len(m.mcpSummaries())-rows)
			switch msg.String() {
			case "m", "esc":
				m.mcpScreen = false
			case "q":
				return m, tea.Quit
			case "up":
				m.mcpOffset--
			case "down":
				m.mcpOffset++
			case "pgup", "pageup":
				m.mcpOffset -= rows
			case "pgdown", "pagedown":
				m.mcpOffset += rows
			case "home":
				m.mcpOffset = 0
			case "end":
				m.mcpOffset = last
			}
			m.mcpOffset = min(last, max(0, m.mcpOffset))
			return m, nil
		}
		if m.latencyScreen {
			summaries := m.latencyStore.Summaries()
			switch msg.String() {
			case "l", "esc":
				m.latencyScreen = false
			case "+", "=":
				m.latencyBuckets = min(100, m.latencyBuckets+1)
				m.recalculateLatencyScale(time.Now())
			case "-":
				m.latencyBuckets = max(2, m.latencyBuckets-1)
				m.recalculateLatencyScale(time.Now())
			case "v":
				m.latencyMode = (m.latencyMode + 1) % latencyDistributionModeCount
			case "c":
				m.recalculateLatencyScale(time.Now())
			case "up":
				m.latencySelected = max(0, m.latencySelected-1)
			case "down":
				m.latencySelected = min(max(0, len(summaries)-1), m.latencySelected+1)
			case "home":
				m.latencySelected = 0
			case "end":
				m.latencySelected = max(0, len(summaries)-1)
			case "pgup", "pageup":
				m.latencySelected = max(0, m.latencySelected-latencyListRows(m.height))
			case "pgdown", "pagedown":
				m.latencySelected = min(max(0, len(summaries)-1), m.latencySelected+latencyListRows(m.height))
			}
			m.latencyOffset = latencyListOffset(m.latencySelected, m.latencyOffset, len(summaries), m.height)
			return m, nil
		}
		if m.usageScreen {
			switch msg.String() {
			case "u", "esc":
				m.usageScreen = false
			case "r":
				return m, m.startUsageRefresh(time.Now())
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
			default:
				if msg.Type == tea.KeyRunes {
					m.searchQuery += string(msg.Runes)
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "q":
			m.stopContentWork()
			if m.stream != nil {
				_ = m.stream.Close()
			}
			return m, tea.Quit
		case "l":
			m.latencyScreen = true
			m.latencySelected = 0
			m.latencyOffset = 0
			m.recalculateLatencyScale(time.Now())
			return m, nil
		case "m":
			m.mcpScreen, m.mcpOffset = true, 0
			return m, nil
		case "M":
			m.includeMCP = !m.includeMCP
			m.refreshVisibleLogs()
		case "u":
			m.usageScreen = true
			return m, m.startUsageRefresh(time.Now())
		case "+", "=":
			m.window = chart.NextWindow(m.window, -1)
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "-":
			m.window = chart.NextWindow(m.window, 1)
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "1":
			m.window = chart.Window5m
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "2":
			m.window = chart.Window15m
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "3":
			m.window = chart.Window1h
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "4":
			m.window = chart.Window3h
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "5":
			m.window = chart.Window6h
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "6":
			m.window = chart.Window12h
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "7":
			m.window = chart.Window24h
			m.refreshVisibleLogs()
			return m, m.requestWindowBackfill()
		case "space", " ":
			m.following = !m.following
			if m.following {
				m.followPulse = 0
				m.selected = max(0, len(m.logs)-1)
				m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
			}
		case "c":
			m.backfillGeneration++
			m.backfillPendingFrom = time.Time{}
			m.historyCoveredFrom = time.Time{}
			m.cancelPreviews()
			m.previewDone, m.previewRetry = nil, nil
			m.logs = nil
			m.allLogs = nil
			m.reducer = gomodel.NewReducer()
			m.logIndex = make(map[string]int)
			m.counted = make(map[string]time.Time)
			m.store = chart.NewStore()
			m.mcpStore = chart.NewStore()
			m.latencyStore = latency.NewStore()
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
			m.followPulse = 0
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
				request := m.logs[m.selected]
				m.openPopup(request)
				if m.client != nil {
					return m, m.loadPopupDetail()
				}
			}
		case "r":
			if m.stream != nil {
				_ = m.stream.Close()
			}
			m.connectionID++
			m.connected, m.connecting = false, true
			m.lastConnectAt = time.Now()
			return m, tea.Batch(connectCmd(m.client, m.lastEventID, m.connectionID), m.requestWindowBackfill(true))
		}
	case connectMsg:
		if msg.connectionID != 0 && msg.connectionID != m.connectionID {
			_ = msg.response.Close()
			return m, nil
		}
		if m.stream != nil {
			_ = m.stream.Close()
		}
		m.stream, m.reader, m.connected, m.connecting, m.err = msg.response, msg.reader, true, false, ""
		return m, readEventCmd(m.reader, m.connectionID)
	case eventMsg:
		if msg.connectionID != 0 && msg.connectionID != m.connectionID {
			return m, nil
		}
		if msg.event.ID != "" {
			m.lastEventID = msg.event.ID
		}
		m.applyEvent(msg.event)
		return m, readEventCmd(m.reader, m.connectionID)
	case backfillMsg:
		if msg.generation != m.backfillGeneration {
			return m, nil
		}
		m.backfillPendingFrom = time.Time{}
		if msg.err != nil {
			m.err = "load audit history: " + msg.err.Error()
			return m, nil
		}
		for _, event := range msg.events {
			m.applyEvent(event)
		}
		sort.SliceStable(m.allLogs, func(i, j int) bool { return m.allLogs[i].Timestamp.Before(m.allLogs[j].Timestamp) })
		m.refreshVisibleLogs()
		m.prune(time.Now())
		if m.historyCoveredFrom.IsZero() || msg.start.Before(m.historyCoveredFrom) {
			m.historyCoveredFrom = msg.start
		}
		if m.following {
			m.selected = max(0, len(m.logs)-1)
			m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
		}
		return m, m.requestWindowBackfill()
	case auditDetailMsg:
		if !m.popup || msg.requestID != m.popupRequestID || msg.generation != m.popupGeneration {
			return m, nil
		}
		m.popupLoading = false
		if msg.err != nil {
			m.popupLoadError = "Could not load full content: " + msg.err.Error() + " (R retry)"
			return m, nil
		}
		m.receivePopupDetail(msg)
		return m, nil
	case previewMsg:
		m.receivePreview(msg)
		return m, nil
	case errMsg:
		if msg.connectionID != 0 && msg.connectionID != m.connectionID {
			return m, nil
		}
		m.connected, m.connecting, m.err = false, false, msg.err.Error()
		if m.stream != nil {
			_ = m.stream.Close()
			m.stream = nil
		}
		return m, refreshCmd()
	case usageMsg:
		if msg.request != m.usageRequest {
			return m, nil
		}
		if msg.err != nil {
			m.usageErrors[msg.provider] = msg.err.Error()
		} else {
			m.usageSnapshots[msg.provider] = msg.snapshot
			delete(m.usageErrors, msg.provider)
		}
		m.usagePending = max(0, m.usagePending-1)
		return m, nil
	case tickMsg:
		m.prune(time.Time(msg))
		if m.latencyScreen && (m.latencyScaleAt.IsZero() || time.Since(m.latencyScaleAt) >= time.Minute) {
			m.recalculateLatencyScale(time.Time(msg))
		}
		var usageCmd tea.Cmd
		if m.usageScreen && time.Since(m.usageRefreshAt) >= time.Minute {
			usageCmd = m.startUsageRefresh(time.Time(msg))
		}
		if m.following {
			m.followPulse = (m.followPulse + 1) % 3
		}
		if m.connected || m.connecting {
			return m, tea.Batch(refreshCmd(), usageCmd)
		}
		if time.Since(m.lastConnectAt) < reconnectInterval {
			return m, tea.Batch(refreshCmd(), usageCmd)
		}
		m.connecting = true
		m.lastConnectAt = time.Now()
		m.connectionID++
		return m, tea.Batch(connectCmd(m.client, m.lastEventID, m.connectionID), m.requestWindowBackfill(true), refreshCmd(), usageCmd)
	case liveLatencyTickMsg:
		if m.hasVisiblePendingRequest() {
			return m, liveLatencyRefreshCmd()
		}
		m.liveLatencyTicker = false
		return m, nil
	}
	return m, nil
}

func (m *Model) startUsageRefresh(at time.Time) tea.Cmd {
	m.usageRefreshAt = at
	m.usageRequest++
	m.usagePending = len(m.usageFetchers)
	return usageRefreshCmd(m.usageFetchers, m.usageRequest)
}

func (m *Model) recalculateLatencyScale(at time.Time) {
	m.latencyScaleMax = latency.RoundedMaxDuration(m.latencyStore.MaxDuration(), m.latencyBuckets)
	m.latencyScaleAt = at
}

func (m *Model) applyEvent(event gomodel.Event) {
	request, err := m.reducer.Apply(event)
	if err != nil {
		m.err = err.Error()
		return
	}
	if request == nil {
		return
	}
	rows := request.LogRows()
	m.replaceRequestRows(request.ID, rows)
	terminalEvent := event.Event == "audit.completed" || event.Event == "audit.failed"
	if (request.Terminal || terminalEvent) && m.counted[request.ID].IsZero() {
		if !request.IsMCP() {
			for _, row := range rows {
				m.store.Add(row.TimestampOrNow(), row.Success)
			}
			m.latencyStore.AddRequest(requestLatencySamples(rows))
		} else {
			if m.mcpStore == nil {
				m.mcpStore = chart.NewStore()
			}
			m.mcpStore.Add(request.TimestampOrNow(), request.Success)
		}
		m.counted[request.ID] = request.TimestampOrNow()
	}
	if m.following {
		m.selected = max(0, len(m.logs)-1)
		m.logOffset = max(0, len(m.logs)-visibleLogRows(m.height))
	}
}

func (m *Model) prune(now time.Time) {
	before := now.Add(-historyRetention)
	if !m.historyCoveredFrom.IsZero() && m.historyCoveredFrom.Before(before) {
		m.historyCoveredFrom = before
	}
	if m.allLogs == nil {
		m.allLogs = append([]gomodel.Request(nil), m.logs...)
	}
	logs := m.allLogs[:0]
	for _, row := range m.allLogs {
		if !row.TimestampOrNow().Before(before) {
			logs = append(logs, row)
		}
	}
	m.allLogs = logs
	m.refreshVisibleLogs()
	for id, at := range m.counted {
		if at.Before(before) {
			delete(m.counted, id)
		}
	}
	m.reducer.Prune(before)
	m.store.Prune(before)
	if m.mcpStore != nil {
		m.mcpStore.Prune(before)
	}
	m.latencyStore.Prune(before)
	m.prunePreviewState()
}

func requestLatencySamples(rows []gomodel.Request) []latency.Sample {
	samples := make([]latency.Sample, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, latency.Sample{
			Key:          row.RoutedModel,
			At:           row.TimestampOrNow(),
			Duration:     row.Duration,
			Success:      row.Success,
			InputTokens:  row.InputTokens,
			OutputTokens: row.OutputTokens,
		})
	}
	return samples
}

func (m Model) View() string {
	if m.width == 0 {
		return "Starting gomodeltui…"
	}
	if m.latencyScreen {
		return m.renderLatencyScreen()
	}
	if m.usageScreen {
		return m.renderUsageScreen()
	}
	if m.mcpScreen {
		return m.renderMCPScreen()
	}
	chartHeight := chartAreaHeight(m.height)
	axisWidth := chartAxisMinimumWidth
	var buckets []chart.Bucket
	now := time.Now()
	for range 3 {
		chartWidth := max(1, (m.width-axisWidth+1)/2)
		buckets = m.store.Snapshot(now, m.window, chartWidth)
		if m.includeMCP && m.mcpStore != nil {
			for index, bucket := range m.mcpStore.Snapshot(now, m.window, chartWidth) {
				buckets[index].Success += bucket.Success
				buckets[index].Errors += bucket.Errors
			}
		}
		nextAxisWidth := chartAxisWidth(chartScale(chart.MaxTotal(buckets)))
		if nextAxisWidth == axisWidth {
			break
		}
		axisWidth = nextAxisWidth
	}
	var success, errors int
	for _, bucket := range buckets {
		success += bucket.Success
		errors += bucket.Errors
	}
	dot := errorStyle.Render("●")
	if m.connected {
		dot = successStyle.Render("●")
	}
	summary := fmt.Sprintf("[%s: ✅ %d / 🚫 %d]", windowLabel(m.window), success, errors)
	left := dot + " " + lipgloss.NewStyle().Bold(true).Render("GoModel") + " " + summary
	if m.includeMCP {
		left += " MCP:on"
	} else {
		left += " MCP:off"
	}
	followLabel := "follow:on"
	if !m.following {
		followLabel = "follow:off"
	}
	keysText := chartHeaderLegend(max(0, m.width-lipgloss.Width(left)-1), followLabel)
	if m.searching {
		keysText = "/: " + m.searchQuery + "  Enter: find  Esc: cancel"
	}
	keys := mutedStyle.Render(truncateText(keysText, max(0, m.width-lipgloss.Width(left)-1)))
	gap := lipgloss.NewStyle().Width(max(1, m.width-lipgloss.Width(left)-lipgloss.Width(keys))).Render("")
	header := left + gap + keys
	chartText := renderChart(buckets, m.width, chartHeight) + "\n" + renderChartXAxis(m.width, axisWidth, time.Duration(m.window)) + "\n" + mutedStyle.Render(strings.Repeat("─", m.width))
	logs := m.renderLogs(m.width)
	if m.popup {
		return m.renderPopup()
	}
	footer := ""
	if m.following {
		footer = mutedStyle.Render(strings.Repeat(".", int(m.followPulse)+1))
	}
	return strings.Join([]string{header, chartText, logs, footer}, "\n")
}

func chartHeaderLegend(width int, followLabel string) string {
	for _, candidate := range []string{
		"1-7: window 5m/15m/1h/3h/6h/12h/24h  +/-: zoom  Space: " + followLabel + "  ↑↓: select  Enter: JSON  /: search  M: toggle MCP  l: latency  u: usage  m: MCP  q: quit",
		"1-7: window 5m/15m/1h/3h/6h/12h/24h  +/-: zoom  M: MCP  l: latency  u: usage  m: MCP",
		"1-7: window  +/-: zoom  M: toggle MCP  l: latency  u: usage  m: MCP",
		"1-7: window  M: MCP  l: latency  u: usage  m: MCP",
		"1-7: window  M: MCP",
		"1-7: window",
	} {
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return ""
}

func (m Model) renderLogs(width int) string {
	rows := visibleLogRows(m.height)
	start := min(m.logOffset, max(0, len(m.logs)-rows))
	end := min(len(m.logs), start+rows)
	contentWidth := max(1, width-2)
	thumbStart, thumbEnd := scrollbarThumb(rows, len(m.logs), start)
	var out []string
	for index, request := range m.logs[start:end] {
		icon := mutedStyle.Render("·")
		if request.Terminal && request.Failover && request.Success {
			icon = failoverStyle.Render("✓")
		} else if request.Terminal && !request.Success {
			icon = errorStyle.Render("✗")
		} else if request.Terminal && request.Success {
			icon = successStyle.Render("✓")
		}
		timestamp := request.Timestamp.Format("15:04:05")
		if request.Timestamp.IsZero() {
			timestamp = "--:--:--"
		}
		responseTimeRendered := mutedStyle.Render("-")
		responseTimeSuffix := ""
		duration := request.Duration
		if !request.Terminal {
			startedAt := request.StartedAt
			if startedAt.IsZero() {
				startedAt = request.Timestamp
			}
			if !startedAt.IsZero() {
				duration = time.Since(startedAt)
			}
		}
		if duration > 0 {
			responseTime := fmt.Sprintf("%.1f", float64(duration)/float64(time.Millisecond))
			responseTimeRendered = responseTimeStyle(duration).Render(responseTime)
			responseTimeSuffix = mutedStyle.Render("ms")
		}
		arrow := mutedStyle.Render(" -> ")
		session := ""
		if len(request.SessionID) > 0 {
			session = sessionStyle(request.SessionID).Render("sid:" + request.SessionID[max(0, len(request.SessionID)-3):])
		}
		route := renderRoute(request.RoutedModel)
		if request.Failover {
			route += mutedStyle.Render(" (failover)")
		}
		status := request.StatusCode
		if status == "" {
			status = "-"
		}
		path := userPathStyle(request.UserPath).Render(request.UserPath)
		clientModel := ""
		if request.ClientModel != "" {
			clientModel = modelStyle(request.ClientModel).Render(request.ClientModel)
		}
		target := strings.Join(nonEmpty(clientModel, route), arrow)
		prefix := icon + " " + mutedStyle.Render(timestamp) + " " + path + " " + session
		if target != "" {
			prefix += arrow + target
		}
		prefix += " " + mutedStyle.Render("i:") + formatTokens(request.InputTokens, request.InputTokensKnown) + " " + mutedStyle.Render("o:") + formatTokens(request.OutputTokens, request.OutputTokensKnown) + " " + mutedStyle.Render("c:") + formatCache(request.CacheRatio, request.CacheRatioKnown) + " " + statusStyle(status).Render(status) + " " + responseTimeRendered + responseTimeSuffix
		if request.IsMCP() {
			prefix = icon + " " + mutedStyle.Render(timestamp) + " " + path + " " + popupToolStyle.Render("MCP") + " " + request.MCPAction() + " " + statusStyle(status).Render(status) + " " + responseTimeRendered + responseTimeSuffix
		}
		if request.Error != "" {
			prefix += " " + collapsePreview(request.Error)
		}
		line := prefix
		if request.LastTurn != "" && !request.IsMCP() {
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

func (m *Model) openPopup(request gomodel.Request) {
	m.cancelPopupDetail()
	m.popupRecord = request
	m.popup = true
	m.popupOffset = 0
	m.popupRaw = false
	m.popupMessage = 0
	m.popupAll = false
	m.popupLoading = false
	m.popupLoadError = ""
	m.popupRequestID = logicalRequestID(request.ID)
	m.popupAuditLogID = request.AuditLogID
	m.popupRawLines = strings.Split(request.RawJSON, "\n")
	if request.IsMCP() {
		m.popupLines = buildMCPPopupLines(request)
		m.popupMessages = nil
		return
	}
	m.popupLines = buildPopupSummaryLines(request.RawJSON)
	m.popupMessages = parsePopupMessages(request.RawJSON)
	if len(m.popupMessages) == 0 {
		return
	}
	m.popupMessage = len(m.popupMessages) - 1
	_, final := popupResponseState(request.RawJSON)
	if final {
		for index := range m.popupMessages {
			m.popupMessages[index].expanded = false
		}
		return
	}
	m.popupOffset = max(0, len(m.popupContentLines())-popupRows(m.height))
}

func logicalRequestID(id string) string {
	if index := strings.Index(id, "/attempt-"); index >= 0 {
		return id[:index]
	}
	return id
}

func bodiesOmitted(raw string) bool {
	var payload struct {
		Data struct {
			BodiesOmitted bool `json:"bodies_omitted"`
		} `json:"data"`
	}
	return json.Unmarshal([]byte(raw), &payload) == nil && payload.Data.BodiesOmitted
}

func (m Model) renderPopup() string {
	lines := m.popupContentLines()
	rows := popupRows(m.height)
	start := min(m.popupOffset, max(0, len(lines)-rows))
	end := min(len(lines), start+rows)
	thumbStart, thumbEnd := scrollbarThumb(rows, len(lines), start)
	header := "Request structured  (r: raw JSON  R: reload content  Enter: expand/collapse  Space: all  Esc: close)"
	if m.popupRaw {
		header = "Raw JSON  (r: structured view  R: reload content  Home: top  End: bottom  ↑↓: scroll  PgUp/PgDn: scroll  Esc: close)"
	}
	if m.popupLoading {
		header = "Loading full request and response content…"
	}
	if m.popupLoadError != "" {
		header = m.popupLoadError
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
		lines = append([]string{}, m.popupLines...)
		if hasResponse, _ := popupResponseState(strings.Join(m.popupRawLines, "\n")); hasResponse {
			lines = append(lines, "")
		}
		lines = append(lines, m.renderPopupMessages()...)
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
	lines := []string{fmt.Sprintf("MESSAGES (%d)", len(m.popupMessages))}
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
	if m.allLogs == nil {
		m.allLogs = append([]gomodel.Request(nil), m.logs...)
	}
	start := len(m.allLogs)
	filtered := make([]gomodel.Request, 0, len(m.allLogs)+len(rows))
	for _, row := range m.allLogs {
		if row.ID == logicalID || strings.HasPrefix(row.ID, logicalID+"/attempt-") {
			if start == len(m.allLogs) {
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
	m.allLogs = filtered
	m.refreshVisibleLogs()
}

func (m *Model) rebuildLogIndex() {
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

func modelStyle(model string) lipgloss.Style { return coloredValueStyle("model:" + model) }

func providerStyle(provider string) lipgloss.Style { return coloredValueStyle("provider:" + provider) }

func coloredValueStyle(value string) lipgloss.Style {
	colors := []string{"39", "75", "99", "141", "171", "178", "208", "35", "44", "64"}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colors[hash.Sum32()%uint32(len(colors))]))
}

func renderRoute(route string) string {
	provider, model, found := strings.Cut(route, "/")
	if !found {
		return modelStyle(provider).Render(provider)
	}
	return providerStyle(provider).Render(provider) + mutedStyle.Render("/") + modelStyle(model).Render(model)
}

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

func responseTimeStyle(duration time.Duration) lipgloss.Style {
	switch {
	case duration < 5*time.Second:
		return successStyle
	case duration <= 30*time.Second:
		return failoverStyle
	default:
		return errorStyle
	}
}

func chartAreaHeight(height int) int { return max(4, height/3-2) }
func visibleLogRows(height int) int  { return max(1, height-chartAreaHeight(height)-6) }
func windowLabel(w chart.Window) string {
	switch w {
	case chart.Window5m:
		return "5m"
	case chart.Window15m:
		return "15m"
	default:
		return fmt.Sprintf("%dh", int(time.Duration(w).Hours()))
	}
}
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

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}
