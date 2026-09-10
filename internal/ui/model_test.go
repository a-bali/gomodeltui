package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/balia/gomodeltui/internal/chart"
	"github.com/balia/gomodeltui/internal/gomodel"
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
