package ui

import (
	"strings"
	"testing"

	"github.com/balia/gomodeltui/internal/gomodel"
	tea "github.com/charmbracelet/bubbletea"
)

const responsesAudit = `{"request_id":"responses-1","type":"audit.completed","data":{"data":{"request_body":{"instructions":"Follow the instructions","input":[{"role":"user","content":[{"type":"input_text","text":"Check this app"}]},{"type":"reasoning","summary":[{"type":"summary_text","text":"Inspect the code"}]},{"type":"function_call","call_id":"call-1","name":"shell","arguments":"{\"cmd\":\"pwd\"}"},{"type":"function_call_output","call_id":"call-1","output":"{\"exit_code\":0,\"output\":\"working directory\"}"}]},"response_body":{"output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Found the issue"}]},{"type":"function_call","call_id":"call-2","name":"read_file","arguments":"{}"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fixed the app"}]}]}}}}`

func TestResponsesPopupFormatsConversationAndOutput(t *testing.T) {
	got := strings.Join(buildPopupLines(responsesAudit), "\n")
	for _, want := range []string{"MESSAGES (5)", "Follow the instructions", "input_text:", "Check this app", "Inspect the code", "tool call: shell", `args: {"cmd":"pwd"}`, "call_id: call-1", "exit_code: 0", "output: working directory", "RESPONSE", "Found the issue", "tool call: read_file", "output_text:", "Fixed the app"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if summary := strings.Join(buildPopupSummaryLines(responsesAudit), "\n"); strings.Contains(summary, "MESSAGES (") || !strings.Contains(summary, "Fixed the app") {
		t.Fatalf("incorrect summary: %s", summary)
	}
}

func TestResponsesPopupOpensAndExpandsToolOutput(t *testing.T) {
	model := NewModel(nil)
	model.width, model.height = 120, 40
	model.logs = []gomodel.Request{{ID: "responses-1", RawJSON: responsesAudit}}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.popup || len(model.popupMessages) != 5 || model.popupMessage != 4 {
		t.Fatalf("popup=%v messages=%d selected=%d", model.popup, len(model.popupMessages), model.popupMessage)
	}
	if model.popupMessages[4].role != "function_call_output" || model.popupMessages[4].expanded {
		t.Fatalf("unexpected tool output state: %+v", model.popupMessages[4])
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.popupMessages[4].expanded || !strings.Contains(model.renderPopup(), "working directory") {
		t.Fatalf("tool output did not expand:\n%s", model.renderPopup())
	}
}

func TestResponsesStringInputPopup(t *testing.T) {
	messages := parsePopupMessages(`{"request_body":{"input":"hello"}}`)
	if len(messages) != 1 || messages[0].role != "user" || !strings.Contains(strings.Join(messages[0].lines, "\n"), "hello") {
		t.Fatalf("messages=%+v", messages)
	}
}
