package gomodel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuditLogsLoadsPagesAndFiltersExactRetention(t *testing.T) {
	start := time.Date(2026, 9, 20, 11, 30, 0, 0, time.UTC)
	var offsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/audit/log" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization=%q", got)
		}
		offsets = append(offsets, r.URL.Query().Get("offset"))
		if r.URL.Query().Get("offset") == "0" {
			_, _ = w.Write([]byte(`{"entries":[{"request_id":"old","timestamp":"2026-09-20T11:29:59Z"},{"request_id":"new","timestamp":"2026-09-20T11:30:00Z","status_code":200}],"total":2,"limit":100,"offset":0}`))
			return
		}
		_, _ = w.Write([]byte(`{"entries":[],"total":2,"limit":100,"offset":2}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.AuditLogs(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Event != "audit.completed" {
		t.Fatalf("events=%+v", events)
	}
	request, err := NewReducer().Apply(events[0])
	if err != nil || request == nil || request.ID != "new" || !request.Success {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	if len(offsets) != 1 || offsets[0] != "0" {
		t.Fatalf("offsets=%v", offsets)
	}
}
