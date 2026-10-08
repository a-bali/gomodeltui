package ui

import (
	"github.com/a-bali/gomodeltui/internal/gomodel"
	"strings"
	"testing"
)

func TestMCPRowAndPopup(t *testing.T) {
	raw := `{"request_id":"tool-1","type":"audit.completed","data":{"provider":"mcp","path":"/mcp","requested_model":"tools/call","status_code":200,"session_id":"secret-session","input_tokens":123,"data":{"request_body":{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"search","arguments":{"messages":[{"role":"user","content":"not a chat"}],"request_body":"nested"}}},"response_body":{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"found"}],"structuredContent":{"count":1},"isError":false}}}}}`
	request, err := gomodel.NewReducer().Apply(gomodel.Event{Event: "audit.completed", Data: []byte(raw)})
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(nil)
	m.width, m.height = 180, 30
	m.logs = []gomodel.Request{*request}
	row := m.renderLogs(m.width)
	for _, want := range []string{"MCP", "tools/call search", "200"} {
		if !strings.Contains(row, want) {
			t.Fatalf("missing %q: %s", want, row)
		}
	}
	for _, unwanted := range []string{"sid:", "i:", "o:", "c:"} {
		if strings.Contains(row, unwanted) {
			t.Fatalf("MCP row contains %q: %s", unwanted, row)
		}
	}
	m.openPopup(*request)
	popup := strings.Join(m.popupLines, "\n")
	for _, want := range []string{"PARAMETERS / ARGUMENTS", "structuredContent", "isError", "found", "nested"} {
		if !strings.Contains(popup, want) {
			t.Fatalf("missing %q: %s", want, popup)
		}
	}
	if len(m.popupMessages) != 0 || strings.Contains(popup, "input_tokens") || strings.Contains(popup, "session_id") {
		t.Fatalf("MCP parsed as model request: %s", popup)
	}
	request.Provider, request.Endpoint = "openai", "/v1/chat/completions"
	m.logs = []gomodel.Request{*request}
	if !strings.Contains(m.renderLogs(m.width), "i:123") {
		t.Fatal("non-MCP token rendering changed")
	}
}
