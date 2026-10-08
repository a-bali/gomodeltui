package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a-bali/gomodeltui/internal/chart"
	"github.com/a-bali/gomodeltui/internal/gomodel"
	"github.com/a-bali/gomodeltui/internal/latency"
	"github.com/a-bali/gomodeltui/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fakeUsageFetcher struct{ snapshot usage.Snapshot }

func (f fakeUsageFetcher) Provider() string                              { return f.snapshot.Provider }
func (f fakeUsageFetcher) Fetch(context.Context) (usage.Snapshot, error) { return f.snapshot, nil }

func TestCompletedAuditEventFeedsChart(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Minute)
	model := NewModel(nil)
	event := gomodel.Event{
		Event: "audit.completed",
		Data:  json.RawMessage(`{"request_id":"req-1","type":"audit.completed","timestamp":"` + at.Format(time.RFC3339) + `","data":{"status_code":200,"requested_model":"virtual-smart","resolved_model":"commandcode-goat/deepseek/deepseek-v4.1-flash","provider_name":"commandcode-goat","user_path":"/"}}`),
	}
	_, _ = model.Update(eventMsg{event: event})
	buckets := model.store.Snapshot(time.Now(), chart.Window15m)
	if buckets[len(buckets)-1].Success != 1 {
		t.Fatalf("expected chart success bucket: %+v", buckets[len(buckets)-1])
	}
	if model.logs[0].ClientModel != "virtual-smart" || model.logs[0].RoutedModel != "commandcode-goat/deepseek/deepseek-v4.1-flash" {
		t.Fatalf("unexpected model routing: %+v", model.logs[0])
	}
	model.width, model.height = 100, 30
	if !strings.Contains(model.View(), "█") {
		t.Fatalf("rendered chart contains no bar:\n%s", model.View())
	}
}

func TestAuditCompletedEventCountsEvenWhenReducerTerminalFlagIsAbsent(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Minute)
	model := NewModel(nil)
	event := gomodel.Event{Event: "audit.completed", Data: json.RawMessage(`{"request_id":"req-2","type":"audit.updated","timestamp":"` + at.Format(time.RFC3339) + `","data":{"status_code":200}}`)}
	_, _ = model.Update(eventMsg{event: event})
	buckets := model.store.Snapshot(time.Now(), chart.Window15m)
	if buckets[len(buckets)-1].Success != 1 {
		t.Fatalf("expected explicit completed event to count: %+v", buckets[len(buckets)-1])
	}
}

func TestRequestRowFormatAndStableUserPathColor(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 160, 30
	model.logs = []gomodel.Request{{Timestamp: time.Date(2026, 9, 10, 12, 42, 6, 0, time.UTC), UserPath: "/team/a", ClientModel: "virtual-smart", RoutedModel: "opencode-go/xiaomi/mimo-v2.5", InputTokens: 123123, CacheRatio: .97, OutputTokens: 3211, StatusCode: "200", Duration: 234200000, Terminal: true, Success: true}}
	got := model.renderLogs(model.width)
	for _, want := range []string{"12:42:06", "/team/a", "virtual-smart", "opencode-go/xiaomi/mimo-v2.5", "i:123123", "o:3211", "c:97%", "200", "234.2ms"} {
		if !strings.Contains(got, want) {
			t.Fatalf("row %q missing %q", got, want)
		}
	}
	if userPathStyle("/team/a").GetForeground() != userPathStyle("/team/a").GetForeground() {
		t.Fatal("user path color is not stable")
	}
	if userPathStyle("/team/a").GetForeground() == userPathStyle("/team/b").GetForeground() {
		t.Fatal("expected distinct user path colors")
	}
}

func TestRequestRowSessionAndLastTurn(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 160, 30
	model.logs = []gomodel.Request{{Timestamp: time.Date(2026, 9, 10, 12, 42, 6, 0, time.UTC), UserPath: "/team/a", SessionID: "session-xyz", ClientModel: "virtual-smart", RoutedModel: "opencode-go/xiaomi/mimo-v2.5", StatusCode: "200", Duration: 234200000, LastTurn: "hello from the latest prompt", Terminal: true, Success: true}}
	row := model.renderLogs(model.width)
	for _, want := range []string{"sid:xyz", "hello from the latest prompt"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row %q missing %q", row, want)
		}
	}
	if sessionStyle("session-xyz").GetForeground() != sessionStyle("session-xyz").GetForeground() {
		t.Fatal("session color is not stable")
	}
	if got := truncateText("abcdefgh", 5); got != "abcd…" {
		t.Fatalf("truncated=%q", got)
	}
	if got := collapsePreview("first line\n\nsecond   line"); got != "first line second line" {
		t.Fatalf("preview=%q", got)
	}
}

func TestErrorPreviewIsCollapsedToOneLogLine(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 300, 30
	model.logs = []gomodel.Request{{
		Timestamp:   time.Date(2026, 9, 10, 12, 42, 6, 0, time.UTC),
		UserPath:    "/agent/hermes",
		ClientModel: "virtual-free",
		RoutedModel: "cerebras/gpt-oss-120b",
		StatusCode:  "400",
		Error:       "messages.2.assistant.reasoning_content: property is unsupported\n\nadditional detail",
		Terminal:    true,
	}}
	row := model.renderLogs(model.width)
	if strings.Contains(row, "\n") {
		t.Fatalf("error preview introduced an extra log line: %q", row)
	}
	if !strings.Contains(row, "property is unsupported additional detail") {
		t.Fatalf("collapsed error missing from row: %q", row)
	}
}

func TestPopupRoutingAttemptsShowFailureDetails(t *testing.T) {
	raw := `{"type":"audit.completed","attempts":[{"provider_name":"opencode-go","model":"deepseek-v4.1-flash","status_code":400,"success":false,"error_type":"invalid_request_error","error_code":"unsupported_parameter","error_message":"response_format is unavailable"},{"provider_name":"opencode-go","model":"mimo-v2.5","status_code":200,"success":true}]}`
	got := strings.Join(buildPopupSummaryLines(raw), "\n")
	for _, want := range []string{"error_type: invalid_request_error", "error_code: unsupported_parameter", "error_message: response_format is unavailable"} {
		if !strings.Contains(got, want) {
			t.Fatalf("popup missing %q:\n%s", want, got)
		}
	}
}

func TestPendingRequestRowIsNotRenderedAsSuccessful(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 160, 30
	model.logs = []gomodel.Request{{
		Timestamp:   time.Date(2026, 9, 10, 12, 42, 6, 0, time.UTC),
		UserPath:    "/agent/hermes",
		SessionID:   "session-xyz",
		RoutedModel: "opencode-go/deepseek-v4.1-flash",
	}}
	row := model.renderLogs(model.width)
	if strings.Contains(row, "✓") || strings.Contains(row, "-ms") {
		t.Fatalf("pending row rendered as completed: %q", row)
	}
	if strings.Contains(row, "->  ->") {
		t.Fatalf("empty model created duplicate arrows: %q", row)
	}
	if !strings.Contains(row, "·") || !strings.Contains(row, " - ") {
		t.Fatalf("pending row lacks pending/status markers: %q", row)
	}
}

func TestStaleConnectionErrorDoesNotTurnCurrentConnectionRed(t *testing.T) {
	model := NewModel(nil)
	model.connected = true
	model.connecting = false
	model.connectionID = 2
	_, _ = model.Update(errMsg{err: context.Canceled, connectionID: 1})
	if !model.connected || model.connecting {
		t.Fatalf("stale connection error changed current state: connected=%v connecting=%v", model.connected, model.connecting)
	}
}

func TestConnectCommandKeepsSSEBodyAliveAfterHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		_, _ = writer.Write([]byte("event: heartbeat\ndata: {}\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client, err := gomodel.NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	message := connectCmd(client, "", 1)()
	connected, ok := message.(connectMsg)
	if !ok {
		t.Fatalf("connect command returned %T, want connectMsg", message)
	}
	defer connected.response.Close()

	eventMessage := readEventCmd(connected.reader, 1)()
	if event, ok := eventMessage.(eventMsg); !ok || event.event.Event != "heartbeat" {
		t.Fatalf("SSE body was not readable after connect: %#v", eventMessage)
	}
}

func TestResponseTimeStyle(t *testing.T) {
	if responseTimeStyle(5*time.Second).GetForeground() != failoverStyle.GetForeground() {
		t.Fatal("5 seconds should be yellow")
	}
	if responseTimeStyle(30*time.Second).GetForeground() != failoverStyle.GetForeground() {
		t.Fatal("30 seconds should be yellow")
	}
	if responseTimeStyle(4999*time.Millisecond).GetForeground() != successStyle.GetForeground() {
		t.Fatal("under 5 seconds should be green")
	}
	if responseTimeStyle(30*time.Second+time.Millisecond).GetForeground() != errorStyle.GetForeground() {
		t.Fatal("over 30 seconds should be red")
	}
}

func TestLatencyScreenListsAndSelectsModels(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 100, 24
	model.latencyStore.AddRequest(requestLatencySamples([]gomodel.Request{{RoutedModel: "provider/a", Duration: time.Second}}))
	model.latencyStore.AddRequest(requestLatencySamples([]gomodel.Request{{RoutedModel: "provider/b", Duration: 2 * time.Second}, {RoutedModel: "provider/b", Duration: 3 * time.Second}}))
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if !model.latencyScreen || !strings.Contains(model.View(), "provider/b") {
		t.Fatalf("latency screen did not open:\n%s", model.View())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.latencySelected != 1 {
		t.Fatalf("selected=%d, want 1", model.latencySelected)
	}
	if !strings.Contains(model.View(), "p50") || !strings.Contains(model.View(), "█") {
		t.Fatalf("latency details missing:\n%s", model.View())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.latencyScreen {
		t.Fatal("latency screen did not close")
	}
}

func TestLatencyHistogramKeepsEmptyCellsAlignedWithLabels(t *testing.T) {
	chart := renderCenteredLatencyHistogram([]int{1, 0, 0, 0, 0, 0, 0, 0, 0, 1}, latencyBucketLabels(100*time.Second, 10), 100, 8)
	lines := strings.Split(chart, "\n")
	if len(lines) == 0 {
		t.Fatal("empty histogram")
	}
	barWidth := lipgloss.Width(lines[0])
	labelWidth := lipgloss.Width(lines[len(lines)-1])
	if barWidth != labelWidth {
		t.Fatalf("bar width=%d, label width=%d; cells are not aligned", barWidth, labelWidth)
	}
	if !strings.Contains(chart, "10s") || !strings.Contains(chart, "100s") {
		t.Fatalf("bucket labels missing: %s", chart)
	}
}

func TestLatencyBucketShortcuts(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 100, 24
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	initial := model.latencyBuckets
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if model.latencyBuckets != initial+1 {
		t.Fatalf("buckets=%d, want %d", model.latencyBuckets, initial+1)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	if model.latencyBuckets != initial {
		t.Fatalf("buckets=%d, want %d", model.latencyBuckets, initial)
	}
}

func TestLatencyRecalculateShortcut(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 100, 24
	model.latencyStore.AddRequest(requestLatencySamples([]gomodel.Request{{RoutedModel: "provider/a", Duration: time.Second}}))
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	model.latencyStore.AddRequest(requestLatencySamples([]gomodel.Request{{RoutedModel: "provider/a", Duration: 10 * time.Second}}))
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if model.latencyScaleMax < 10*time.Second {
		t.Fatalf("scale=%s, want at least 10s", model.latencyScaleMax)
	}
}

func TestLatencyTableUsesTerminalWidth(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 100, 24
	model.latencyStore.AddRequest(requestLatencySamples([]gomodel.Request{{RoutedModel: "provider/model", Duration: time.Second}}))
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	lines := strings.Split(model.View(), "\n")
	for _, line := range lines[1:3] {
		if got := lipgloss.Width(line); got != model.width {
			t.Fatalf("table line width=%d, want %d: %q", got, model.width, line)
		}
	}
}

func TestLatencyTablePreservesLongValuesAndRightAlignsColumns(t *testing.T) {
	durations := make([]time.Duration, 21)
	for index := range durations {
		durations[index] = time.Duration(index+1) * time.Second
	}
	summary := latency.Summary{Key: "commandcode-goat/meta/muse-spark-1.3-contributor", Attempts: 40, LogicalRequests: 41, Success: 42, Errors: 43, Durations: durations}
	table := newLatencyTable([]latency.Summary{summary}, 120)
	header, row := table.header(), table.row(summary)
	if strings.Contains(row, "…") {
		t.Fatalf("unexpected truncated row: %q", row)
	}
	for _, column := range []string{"attempts", "logical", "ok", "err", "p50", "p95", "max"} {
		headerEnd := strings.Index(header, column) + len(column)
		valueEnd := strings.Index(row, latencyTableValues(summary)[indexOf(latencyMetricHeaders, column)+1]) + len(latencyTableValues(summary)[indexOf(latencyMetricHeaders, column)+1])
		if headerEnd != valueEnd {
			t.Fatalf("%s is not right-aligned: header=%q row=%q", column, header, row)
		}
	}
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}

func TestFailoverExpandsToFailedAndFailoverRows(t *testing.T) {
	request := gomodel.Request{ID: "req-1", ClientModel: "virtual-smart", Terminal: true, Success: true, Attempts: []gomodel.Attempt{{Seq: 1, ProviderName: "openai", Model: "gpt-4o", StatusCode: 503, ErrorType: "upstream", Success: false}, {Seq: 2, ProviderName: "openai", Model: "gpt-4o", StatusCode: 503, ErrorType: "upstream", Success: false}, {Seq: 3, ProviderName: "anthropic", Model: "claude-sonnet", StatusCode: 200, Success: true}}}
	rows := request.LogRows()
	if len(rows) != 2 || rows[0].Success || rows[0].StatusCode != "503" || !rows[1].Success || !rows[1].Failover {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if rows[1].RoutedModel != "anthropic/claude-sonnet" {
		t.Fatalf("route=%q", rows[1].RoutedModel)
	}
	model := NewModel(nil)
	model.width, model.height = 160, 30
	model.logs = rows
	if !strings.Contains(model.renderLogs(model.width), "(failover)") {
		t.Fatal("missing failover marker")
	}
}

func TestFiveMinuteWindowShortcut(t *testing.T) {
	model := NewModel(nil)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if model.window != chart.Window5m {
		t.Fatalf("window=%v", model.window)
	}
}

func TestMainHeaderShowsWindowLegendAndSummary(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 160, 30
	model.window = chart.Window6h
	view := model.View()
	if !strings.Contains(view, "GoModel") || strings.Contains(view, "GoModel TUI") {
		t.Fatalf("unexpected title: %s", view)
	}
	if !strings.Contains(view, "1-7 window 5m/15m/1h/3h/6h/12h/24h") {
		t.Fatalf("window legend missing: %s", view)
	}
	if !strings.Contains(view, "[6h: ✅ 0 / 🚫 0]") {
		t.Fatalf("window summary missing: %s", view)
	}
	if strings.Contains(view, "successful") || strings.Contains(view, "requests/bin") {
		t.Fatalf("obsolete second-row legend remains: %s", view)
	}
}

func TestMainScreenDividerUsesTerminalWidth(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 79, 20
	if !strings.Contains(model.View(), strings.Repeat("─", model.width)) {
		t.Fatalf("full-width divider missing:\n%s", model.View())
	}
}

func TestUsageScreenOpensAndDisplaysSnapshots(t *testing.T) {
	fetcher := fakeUsageFetcher{snapshot: usage.Snapshot{Provider: "OpenCode Go", Source: "API key", Windows: []usage.Window{{Label: "5h", Used: 12.5, ResetsAt: time.Date(2026, 9, 21, 12, 6, 0, 0, time.Local)}}}}
	model := NewModel(nil, fetcher)
	model.width, model.height = 100, 24
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if !model.usageScreen || !strings.Contains(model.View(), "OpenCode Go: refreshing") {
		t.Fatalf("usage screen did not open:\n%s", model.View())
	}
	_, _ = model.Update(usageMsg{provider: fetcher.Provider(), snapshot: fetcher.snapshot, request: model.usageRequest})
	if !strings.Contains(model.View(), "12.5% used") || !strings.Contains(model.View(), "API key") || !strings.Contains(model.View(), "2026-09-21 12:06") {
		t.Fatalf("usage snapshot did not render:\n%s", model.View())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if view := model.View(); !strings.Contains(view, "OpenCode Go: refreshing") || strings.Contains(view, "12.5% used") {
		t.Fatalf("refresh did not replace stale usage:\n%s", view)
	}
}

func TestFormatUsageResetShowsDateAndRemainingTime(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.Local)
	at := now.Add(26*time.Hour + 14*time.Minute)
	if got := formatUsageReset(at, now); got != "resets 2026-09-21 12:14 (in 1d 2h 14m)" {
		t.Fatalf("reset=%q", got)
	}
}

func TestUsageWindowAddsResponsiveProgressBar(t *testing.T) {
	reset := "resets 2026-09-21 12:06 (in 2h)"
	wide := renderUsageWindow(100, "5h", 50, reset, usageProgressWidth(100, "5h", reset))
	if !strings.Contains(wide, "50.0% used") || !strings.Contains(wide, "[") || !strings.Contains(wide, reset) {
		t.Fatalf("wide usage line missing progress bar or details: %q", wide)
	}
	if got := lipgloss.Width(wide); got != 100 {
		t.Fatalf("wide usage line width=%d, want 100", got)
	}
	narrow := renderUsageWindow(45, "5h", 50, reset, usageProgressWidth(45, "5h", reset))
	if strings.Contains(narrow, "[") || !strings.Contains(narrow, reset) {
		t.Fatalf("narrow usage line should retain details without a bar: %q", narrow)
	}
}

func TestUsageProgressBarsShareTheMostConstrainedWidth(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 0, 0, 0, time.Local)
	first := fakeUsageFetcher{snapshot: usage.Snapshot{Provider: "First", Windows: []usage.Window{{Label: "5h", ResetsAt: now.Add(time.Hour)}}}}
	second := fakeUsageFetcher{snapshot: usage.Snapshot{Provider: "Second", Windows: []usage.Window{{Label: "monthly", ResetsAt: now.Add(7 * 24 * time.Hour)}}}}
	model := NewModel(nil, first, second)
	model.width = 100
	model.usageSnapshots[first.Provider()] = first.snapshot
	model.usageSnapshots[second.Provider()] = second.snapshot
	got := model.sharedUsageProgressWidth(now)
	want := min(
		usageProgressWidth(model.width, "5h", usageWindowReset(first.snapshot.Windows[0], now)),
		usageProgressWidth(model.width, "monthly", usageWindowReset(second.snapshot.Windows[0], now)),
	)
	if got != want {
		t.Fatalf("shared progress width=%d, want %d", got, want)
	}
}

func TestLayoutAllocatesOneThirdChartAndTwoThirdsLogs(t *testing.T) {
	if got := chartAreaHeight(30); got != 8 {
		t.Fatalf("chart height=%d", got)
	}
	if got := visibleLogRows(30); got != 16 {
		t.Fatalf("log rows=%d, want 16 with chart axis and follow footer", got)
	}
}

func TestFollowFooterPulsesOnRefresh(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 80, 12
	first := model.View()
	_, _ = model.Update(tickMsg(time.Now()))
	second := model.View()
	_, _ = model.Update(tickMsg(time.Now()))
	third := model.View()
	for _, view := range []string{first, second, third} {
		if !strings.Contains(view, ".") {
			t.Fatalf("follow footer missing:\n%s", view)
		}
	}
	if first == second || second == third {
		t.Fatalf("follow footer did not pulse")
	}

	model.following = false
	if strings.HasSuffix(model.View(), ".") {
		t.Fatal("follow footer shown while follow mode is off")
	}
}

func TestFollowModeAndRequestNavigation(t *testing.T) {
	model := NewModel(nil)
	model.height = 12
	model.logs = []gomodel.Request{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	model.following = true
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.following || model.selected != 1 {
		t.Fatalf("navigation state: following=%v selected=%d", model.following, model.selected)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if model.selected != 2 {
		t.Fatalf("page selection=%d", model.selected)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !model.following || model.selected != 2 {
		t.Fatalf("follow state: following=%v selected=%d", model.following, model.selected)
	}
}

func TestConnectionLossStaysInDisconnectedStateUntilReconnectInterval(t *testing.T) {
	model := NewModel(nil)
	model.connected = true
	model.connecting = false
	_, _ = model.Update(errMsg{err: context.Canceled})
	if model.connected || model.connecting {
		t.Fatalf("connection state after error: connected=%v connecting=%v", model.connected, model.connecting)
	}

	model.lastConnectAt = time.Now()
	_, cmd := model.Update(tickMsg(time.Now()))
	if model.connecting || cmd == nil {
		t.Fatalf("unexpected early reconnect state: connecting=%v cmd=%v", model.connecting, cmd == nil)
	}

	model.lastConnectAt = time.Now().Add(-reconnectInterval)
	_, cmd = model.Update(tickMsg(time.Now()))
	if !model.connecting || cmd == nil {
		t.Fatalf("expected reconnect attempt: connecting=%v cmd=%v", model.connecting, cmd == nil)
	}
}

func TestManualReconnectStartsImmediately(t *testing.T) {
	model := NewModel(nil)
	model.connected = false
	model.connecting = false
	model.lastConnectAt = time.Now()
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !model.connecting || cmd == nil {
		t.Fatalf("manual reconnect did not start: connecting=%v cmd=%v", model.connecting, cmd == nil)
	}
}

func TestSelectedLogRowFillsTheLogWidth(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 80, 12
	model.logs = []gomodel.Request{{ID: "1", ClientModel: "virtual-smart", RoutedModel: "provider/model", StatusCode: "200", Terminal: true, Success: true}}
	if got := lipgloss.Width(model.renderLogs(80)); got != 80 {
		t.Fatalf("selected row width=%d, want 80", got)
	}
	if selectionStyle.GetBackground() == nil {
		t.Fatal("selection style has no background")
	}
}

func TestPopupHomeAndEndScroll(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 80, 12
	model.popup = true
	model.popupLines = make([]string, 30)
	model.popupOffset = 4
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	if model.popupOffset != 0 {
		t.Fatalf("home offset=%d", model.popupOffset)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if model.popupOffset != len(model.popupLines)-popupRows(model.height) {
		t.Fatalf("end offset=%d", model.popupOffset)
	}
	popup := model.renderPopup()
	if strings.Contains(popup, "╭") || !strings.Contains(popup, "│") && !strings.Contains(popup, "█") {
		t.Fatalf("popup frame/scrollbar layout is wrong: %q", popup)
	}
}

func TestLogHomeAndEndNavigation(t *testing.T) {
	model := NewModel(nil)
	model.height = 12
	model.logs = []gomodel.Request{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	model.selected = 1
	model.logOffset = 1
	model.following = true
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	if model.following || model.selected != 0 || model.logOffset != 0 {
		t.Fatalf("home state: following=%v selected=%d offset=%d", model.following, model.selected, model.logOffset)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if model.following || model.selected != 2 {
		t.Fatalf("end state: following=%v selected=%d", model.following, model.selected)
	}
}

func TestLogRowsArePrunedByRetentionAndHaveScrollbar(t *testing.T) {
	model := NewModelWithRetention(nil, time.Hour)
	model.width, model.height = 80, 12
	rows := make([]gomodel.Request, 1020)
	for index := range rows {
		rows[index].ID = fmt.Sprintf("%d", index)
		rows[index].Timestamp = time.Now()
	}
	model.replaceRequestRows("new", rows)
	model.selected = len(model.logs) - 1
	model.logOffset = len(model.logs) - visibleLogRows(model.height)
	if len(model.logs) != len(rows) {
		t.Fatalf("log count=%d, want %d", len(model.logs), len(rows))
	}
	model.logs[0].Timestamp = time.Now().Add(-2 * time.Hour)
	model.prune(time.Now())
	if len(model.logs) != len(rows)-1 {
		t.Fatalf("log count after prune=%d, want %d", len(model.logs), len(rows)-1)
	}
	thumbStart, thumbEnd := scrollbarThumb(10, len(model.logs), 1000)
	if thumbEnd <= thumbStart {
		t.Fatal("missing scrollbar thumb")
	}
	if !strings.Contains(model.renderLogs(model.width), "│") && !strings.Contains(model.renderLogs(model.width), "█") {
		t.Fatal("missing log scrollbar")
	}
}

func TestLongErrorDoesNotOverlapLogScrollbar(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 80, 12
	model.logs = []gomodel.Request{{Terminal: true, StatusCode: "500", Error: strings.Repeat("provider failure ", 20)}}
	line := strings.Split(model.renderLogs(model.width), "\n")[0]
	if got := lipgloss.Width(line); got != model.width {
		t.Fatalf("rendered line width=%d, want %d", got, model.width)
	}
}

func TestSearchCoversDisplayedFieldsAndRawJSON(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 80, 12
	model.logs = []gomodel.Request{{ID: "1", ClientModel: "virtual-smart"}, {ID: "2", RawJSON: `{"function":"exec"}`}}
	model.selected = 0
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e', 'x', 'e', 'c'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.searching || model.selected != 1 {
		t.Fatalf("search state: searching=%v selected=%d", model.searching, model.selected)
	}
}

func TestJSONPopupCanOpenScrollAndDismiss(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 80, 12
	model.logs = []gomodel.Request{{ID: "1", RawJSON: "{\n  \"request_id\": \"1\"\n}"}}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.popup || !strings.Contains(model.renderPopup(), "request_id") {
		t.Fatal("popup did not open")
	}
	if got := lipgloss.Width(model.renderPopup()); got != model.width {
		t.Fatalf("popup width=%d, want %d", got, model.width)
	}
	if got := highlightJSONLine(`  "ok": true, "count": 12`); !strings.Contains(got, "ok") || !strings.Contains(got, "true") || !strings.Contains(got, "12") {
		t.Fatalf("highlighted JSON lost content: %q", got)
	}
	model.popupLines = []string{`{"long_prompt":"` + strings.Repeat("word ", 30) + `"}`}
	wrapped := model.popupContentLines()
	if len(wrapped) < 2 {
		t.Fatal("popup JSON was not wrapped")
	}
	original := model.popupLines[0]
	if got := strings.Join(wrapped, ""); got != original {
		t.Fatalf("wrapping changed JSON content: got %q, want %q", got, original)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if model.popup {
		t.Fatal("popup did not dismiss")
	}
}

func TestBackfilledPopupLazilyLoadsContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/audit/detail" || r.URL.Query().Get("log_id") != "audit-1" {
			t.Fatalf("request=%s", r.URL)
		}
		_, _ = w.Write([]byte(`{"id":"audit-1","request_id":"req-1","timestamp":"2026-09-20T11:30:00Z","data":{"request_body":{"messages":[{"role":"user","content":"loaded prompt"}]},"response_body":{"choices":[{"message":{"content":"loaded response"}}]}}}`))
	}))
	defer server.Close()
	client, err := gomodel.NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(client)
	model.width, model.height = 80, 12
	model.logs = []gomodel.Request{{ID: "req-1", AuditLogID: "audit-1", RawJSON: `{"request_id":"req-1","data":{"bodies_omitted":true}}`}}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.popupLoading || !strings.Contains(model.renderPopup(), "Loading full request and response content") {
		t.Fatalf("popup did not show loading state: %q", model.renderPopup())
	}
	message := command()
	_, _ = model.Update(message)
	if model.popupLoading || !strings.Contains(strings.Join(model.popupRawLines, "\n"), "loaded prompt") || !strings.Contains(strings.Join(model.popupContentLines(), "\n"), "loaded response") {
		t.Fatalf("popup did not refresh with detail: %q", strings.Join(model.popupContentLines(), "\n"))
	}
}

func TestPopupInspectorExtractsMessagesAndToolCalls(t *testing.T) {
	raw := `{"request_id":"req-1","data":{"requested_model":"virtual-smart","request_body":{"messages":[{"role":"system","content":"rules"},{"role":"assistant","tool_calls":[{"function":{"name":"exec","arguments":"{\"cmd\":\"pwd\"}"}}]}]}}}`
	lines := buildPopupLines(raw)
	got := strings.Join(lines, "\n")
	for _, want := range []string{"REQUEST", "MESSAGES (2)", "[1] system", "[2] assistant", "tool call: exec", "args:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("inspector missing %q in %q", want, got)
		}
	}
	withResponse := `{"data":{"request_body":{"messages":[]},"response_body":{"choices":[{"message":{"content":"answer"}}]}}}`
	if got := strings.Join(buildPopupSummaryLines(withResponse), "\n"); !strings.Contains(got, "RESPONSE") || !strings.Contains(got, "answer") {
		t.Fatalf("response section missing: %q", got)
	}
}

func TestPopupTabsMessageSelectionAndExpansion(t *testing.T) {
	raw := `{"request_id":"req-1","data":{"request_body":{"messages":[{"role":"system","content":"rules"},{"role":"user","content":"hello"},{"role":"tool","content":"{\"exit_code\":0,\"output\":\"done\"}"}]}}}`
	model := NewModel(nil)
	model.width, model.height = 80, 8
	model.logs = []gomodel.Request{{RawJSON: raw}}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(model.popupMessages) != 3 || model.popupMessages[0].expanded {
		t.Fatalf("popup defaults: messages=%d expanded=%v", len(model.popupMessages), model.popupMessages[0].expanded)
	}
	if !model.popupMessages[2].expanded || strings.Contains(strings.Join(model.popupLines, "\n"), "Messages tab") {
		t.Fatal("popup default expansion or stale tab text")
	}
	if model.popupMessage != 2 || model.popupOffset == 0 {
		t.Fatalf("popup did not start at last message: selected=%d offset=%d", model.popupMessage, model.popupOffset)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.popupMessages[0].expanded {
		t.Fatal("Enter did not expand selected message")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.popupMessage != 1 {
		t.Fatalf("message selection=%d", model.popupMessage)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if model.popupMessages[1].expanded {
		t.Fatal("Left did not collapse selected message")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if !model.popupMessages[1].expanded {
		t.Fatal("Right did not expand selected message")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	for index, message := range model.popupMessages {
		if !message.expanded {
			t.Fatalf("message %d did not expand with Space", index)
		}
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !model.popupRaw {
		t.Fatalf("raw view state: raw=%v", model.popupRaw)
	}
}

func TestPopupWithResponseStaysAtTop(t *testing.T) {
	raw := `{"type":"audit.completed","data":{"request_body":{"messages":[{"role":"user","content":"hello"}]},"response_body":{"choices":[{"message":{"content":"answer"}}]}}}`
	model := NewModel(nil)
	model.width, model.height = 80, 8
	model.logs = []gomodel.Request{{RawJSON: raw}}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, final := popupResponseState(raw)
	if model.popupOffset != 0 || !popupHasResponse(raw) || !final {
		t.Fatalf("response popup position: offset=%d has_response=%v final=%v", model.popupOffset, popupHasResponse(raw), final)
	}
}

func TestPopupShowsThinkingAndSeparatesMessages(t *testing.T) {
	raw := `{"type":"audit.updated","data":{"request_body":{"messages":[{"role":"assistant","thinking":"checking tools","content":"answer"}]},"response_body":{"partial":true}}}`
	model := NewModel(nil)
	model.width, model.height = 80, 12
	model.logs = []gomodel.Request{{RawJSON: raw}}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := strings.Join(model.popupContentLines(), "\n")
	if !strings.Contains(got, "MESSAGES (1)") || !strings.Contains(got, "thinking:") {
		t.Fatalf("missing message header/thinking: %q", got)
	}
	if !strings.Contains(got, "RESPONSE\n") {
		t.Fatalf("missing response section: %q", got)
	}
}
