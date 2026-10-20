package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/a-bali/gomodeltui/internal/chart"
	"github.com/a-bali/gomodeltui/internal/gomodel"
	"github.com/a-bali/gomodeltui/internal/latency"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// PreviewView renders the main TUI with deterministic, synthetic activity for
// documentation screenshots. It uses the same View and chart/log renderers as
// the interactive application and performs no network requests.
func PreviewView(width, height int) string {
	return PreviewScreen("main", width, height)
}

// PreviewScreen renders a named TUI screen with deterministic synthetic data.
func PreviewScreen(screen string, width, height int) string {
	lipgloss.SetColorProfile(termenv.TrueColor)
	now := time.Now()
	m := NewModel(nil)
	m.width, m.height = width, height
	m.connected, m.connecting, m.following = true, false, true
	m.lastConnectAt = now
	m.window = chart.Window1h
	m.store = chart.NewStore()
	for i := 0; i < 76; i++ {
		at := now.Add(-time.Duration(59-i%60) * time.Minute).Add(time.Duration((i*13)%50) * time.Second)
		m.store.Add(at, i%13 != 5 && i%17 != 2)
	}
	paths := []string{"/app/desktop", "/agent/research", "/app/desktop", "/app/terminal", "/agent/research", "/app/desktop", "/app/terminal", "/app/desktop"}
	models := []string{"claude-sonnet-4-5", "gpt-5", "deepseek-v4.1-flash", "claude-sonnet-4-5", "gpt-5", "claude-opus-4-1", "deepseek-v4.1-flash", "gpt-5"}
	turns := []string{
		"Summarize the deployment notes and highlight the two changes that need follow-up.",
		"Review this patch for race conditions and explain the smallest safe fix.",
		"Find the relevant API contract and draft a concise implementation plan.",
		"Compare the latest run with the previous release and list any regressions.",
		"Trace this error through the request path and suggest a focused diagnostic.",
		"Turn these meeting notes into decisions, owners, and next steps.",
		"Check the migration for compatibility with existing configuration files.",
		"Explain why this test is flaky and propose a deterministic replacement.",
	}
	for i := range paths {
		at := now.Add(-time.Duration(2+i*4) * time.Minute)
		duration := time.Duration(380+i*173) * time.Millisecond
		success := i != 5
		r := gomodel.Request{
			ID: fmt.Sprintf("preview-%02d", i), Timestamp: at, StartedAt: at.Add(-duration),
			UserPath: paths[i], SessionID: fmt.Sprintf("session-preview-%03d", 120+i),
			ClientModel: "auto", RoutedModel: models[i], Model: models[i], Provider: "OpenCode Go",
			StatusCode: "200", InputTokens: 1234 + i*816, OutputTokens: 82 + i*47,
			CacheRatio: .72 + float64(i%4)*.06, InputTokensKnown: true, OutputTokensKnown: true,
			CacheRatioKnown: true, Duration: duration, Terminal: true, Success: success, LastTurn: turns[i],
		}
		if i == 5 {
			r.StatusCode, r.Error, r.Success = "429", "Rate limit exceeded", false
		} else if i == 7 {
			r.Terminal, r.Success, r.Duration, r.StatusCode = false, false, 0, ""
			r.StartedAt = now.Add(-7 * time.Second)
		}
		m.logs = append(m.logs, r)
	}
	m.selected = 4
	switch screen {
	case "latency":
		m.latencyScreen = true
		m.latencyBuckets = 30
		m.latencySelected = 0
		m.latencyStore = latency.NewStore()
		keys := []string{"opencode-go/deepseek-v4.1-flash", "anthropic/claude-sonnet-4-5", "openai/gpt-5", "xiaomi/mimo-v2.5"}
		for i := 0; i < 520; i++ {
			key := keys[i%len(keys)]
			duration := time.Duration(300+(i*73)%5700) * time.Millisecond
			if i%29 == 0 {
				duration += time.Duration(8+i%17) * time.Second
			}
			m.latencyStore.AddRequest([]latency.Sample{{Key: key, At: now.Add(-time.Duration(i) * time.Minute), Duration: duration, Success: i%19 != 5}})
		}
	case "request":
		m.popup = true
		m.popupRequestID = "demo-004"
		m.popupRecord = m.logs[4]
		requestJSON, _ := json.MarshalIndent(map[string]any{
			"request_id": "demo-004", "timestamp": m.logs[4].Timestamp.Format(time.RFC3339),
			"user_path": m.logs[4].UserPath, "session_id": m.logs[4].SessionID,
			"requested_model": m.logs[4].ClientModel, "resolved_model": m.logs[4].RoutedModel,
			"provider_name": "OpenCode Go", "status_code": 200, "input_tokens": m.logs[4].InputTokens,
			"output_tokens": m.logs[4].OutputTokens, "cached_input_ratio": m.logs[4].CacheRatio,
			"request_body": map[string]any{"model": m.logs[4].ClientModel, "messages": []any{
				map[string]any{"role": "system", "content": "You are a concise coding assistant. Preserve existing behavior."},
				map[string]any{"role": "user", "content": m.logs[4].LastTurn},
			}}, "response_body": map[string]any{"output_text": "The change is safe. Keep the validation in the request handler and add a focused regression test."},
		}, "", "  ")
		raw := string(requestJSON)
		m.popupRawLines = strings.Split(raw, "\n")
		m.popupLines = buildPopupSummaryLines(raw)
		m.popupMessages = parsePopupMessages(raw)
		m.popupMessage = 1
		if len(m.popupMessages) > 1 {
			m.popupMessages[1].expanded = true
		}
	}
	return m.View()
}
