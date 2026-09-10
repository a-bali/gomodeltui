package gomodel

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCanonicalModelRemovesProviderPrefix(t *testing.T) {
	if got := canonicalModel("commandcode-goat/deepseek/deepseek-v4.1-flash", "commandcode-goat"); got != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("got %q", got)
	}
}

func TestReducerCollapsesLifecycle(t *testing.T) {
	r := NewReducer()
	for _, raw := range []string{
		`{"seq":1,"request_id":"req-1","type":"audit.started","timestamp":"2026-09-10T12:00:00Z","data":{"requested_model":"gpt-4o","provider":"openai","user_path":"/team/a","session_id":"session-xyz"}}`,
		`{"seq":2,"request_id":"req-1","type":"audit.completed","timestamp":"2026-09-10T12:00:01Z","data":{"status_code":200,"duration_ns":120000000,"input_tokens":12,"output_tokens":8,"data":{"request_body":{"messages":[{"role":"user","content":"hello from the latest prompt"}]}}}}`,
	} {
		event := Event{Event: "audit.completed", Data: json.RawMessage(raw)}
		request, err := r.Apply(event)
		if err != nil {
			t.Fatal(err)
		}
		if request == nil {
			t.Fatal("expected request")
		}
		if request.Terminal && !request.Success {
			t.Fatalf("expected success: %+v", request)
		}
	}
	request, _ := r.Apply(Event{Event: "usage.completed", Data: json.RawMessage(`{"request_id":"req-1","type":"usage.completed","data":{"input_tokens":12,"output_tokens":8}}`)})
	if request == nil || !request.Terminal {
		t.Fatal("usage event should update existing request")
	}
	if request.UserPath != "/team/a" || request.Model != "gpt-4o" || request.Provider != "openai" || request.StatusCode != "200" {
		t.Fatalf("unexpected request metadata: %+v", request)
	}
	if request.ClientModel != "gpt-4o" || request.RoutedModel != "openai/gpt-4o" {
		t.Fatalf("unexpected model routing: %+v", request)
	}
	if request.SessionID != "session-xyz" || request.LastTurn != "hello from the latest prompt" {
		t.Fatalf("unexpected session/turn: %+v", request)
	}
	if request.InputTokens != 12 || request.OutputTokens != 8 || request.Duration != 120*time.Millisecond {
		t.Fatalf("unexpected token counts: %+v", request)
	}
	heartbeatRequest, _ := r.Apply(Event{Event: "heartbeat", Data: json.RawMessage(`{}`)})
	if heartbeatRequest != nil {
		t.Fatal("heartbeat should not produce a row")
	}
}
