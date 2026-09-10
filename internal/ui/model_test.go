package ui

import (
	"encoding/json"
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
		Data:  json.RawMessage(`{"request_id":"req-1","type":"audit.completed","timestamp":"` + at.Format(time.RFC3339) + `","data":{"status_code":200,"resolved_model":"commandcode-goat/deepseek/deepseek-v4.1-flash","provider_name":"commandcode-goat","user_path":"/"}}`),
	}
	_, _ = model.Update(eventMsg{event: event})
	buckets := model.store.Snapshot(at, chart.Window15m)
	if buckets[len(buckets)-1].Success != 1 {
		t.Fatalf("expected chart success bucket: %+v", buckets[len(buckets)-1])
	}
	if model.logs[0].Model != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("unexpected model: %q", model.logs[0].Model)
	}
}
