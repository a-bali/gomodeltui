package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/balia/gomodeltui/internal/gomodel"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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

func TestLayoutAllocatesOneThirdChartAndTwoThirdsLogs(t *testing.T) {
	if got := chartAreaHeight(30); got != 8 {
		t.Fatalf("chart height=%d", got)
	}
	if got := visibleLogRows(30); got != 18 {
		t.Fatalf("log rows=%d", got)
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
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if model.popup {
		t.Fatal("popup did not dismiss")
	}
}
