package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/balia/gomodeltui/internal/gomodel"
	tea "github.com/charmbracelet/bubbletea"
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
