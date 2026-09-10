package gomodel

import (
	"encoding/json"
	"testing"
)

func TestReducerCollapsesLifecycle(t *testing.T) {
	r := NewReducer()
	for _, raw := range []string{
		`{"seq":1,"request_id":"req-1","type":"audit.started","data":{"requested_model":"gpt-4o","provider":"openai"}}`,
		`{"seq":2,"request_id":"req-1","type":"audit.completed","data":{"status_code":200,"duration_ns":120000000}}`,
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
	request, _ := r.Apply(Event{Event: "heartbeat", Data: json.RawMessage(`{}`)})
	if request != nil {
		t.Fatal("heartbeat should not produce a row")
	}
}
