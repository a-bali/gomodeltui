package ui

import (
	"fmt"
	"github.com/balia/gomodeltui/internal/chart"
	"github.com/balia/gomodeltui/internal/gomodel"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"
)

func TestMCPStatsExcludeModelMetricsAndRetainHistory(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 150, 20
	now := time.Now()
	event := func(id, provider, method string, status, duration int, body string) gomodel.Event {
		return gomodel.Event{Event: "audit.completed", Data: []byte(fmt.Sprintf(`{"request_id":%q,"timestamp":%q,"data":{"provider":%q,"requested_model":%q,"status_code":%d,"duration_ms":%d,"data":%s}}`, id, now.Format(time.RFC3339Nano), provider, method, status, duration, body))}
	}
	first := event("1", "mcp", "ping", 200, 10, `{}`)
	m.applyEvent(first)
	m.applyEvent(first)
	m.applyEvent(event("2", "mcp", "ping", 500, 30, `{}`))
	m.applyEvent(event("3", "mcp", "tools/call", 200, 20, `{"response_body":{"result":{"isError":true}}}`))
	m.applyEvent(event("4", "mcp", "initialize", 200, 0, `{}`))
	m.applyEvent(event("5", "openai", "model", 200, 50, `{}`))
	total := 0
	for _, bucket := range m.store.Snapshot(now.Add(time.Second), chart.Window1h) {
		total += bucket.Total()
	}
	if total != 1 || len(m.latencyStore.Summaries()) != 1 {
		t.Fatalf("MCP leaked into model metrics: chart=%d latency=%v", total, m.latencyStore.Summaries())
	}
	summaries := m.mcpSummaries()
	if len(summaries) != 3 || summaries[0].Key != "ping" || summaries[0].Attempts != 2 || summaries[0].Success != 1 || summaries[0].Errors != 1 {
		t.Fatalf("bad MCP counts/order: %+v", summaries)
	}
	if summaries[0].Durations[0] != 10*time.Millisecond || summaries[0].Durations[1] != 30*time.Millisecond {
		t.Fatal("bad MCP timings")
	}
	for _, s := range summaries {
		if s.Key == "tools/call" && s.Errors != 1 {
			t.Fatal("tool error with HTTP 200 was not counted")
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if !m.mcpScreen || !strings.Contains(m.View(), "MCP methods") || !strings.Contains(m.View(), "p95") {
		t.Fatal("MCP screen did not open")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.mcpScreen {
		t.Fatal("MCP screen did not close")
	}
	m.prune(now.Add(2 * time.Hour))
	if len(m.mcpSummaries()) != 0 {
		t.Fatal("expired MCP stats retained")
	}
}
