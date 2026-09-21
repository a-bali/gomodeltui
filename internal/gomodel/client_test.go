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
			_, _ = w.Write([]byte(`{"entries":[{"id":"audit-old","request_id":"old","timestamp":"2026-09-20T11:29:59Z"},{"id":"audit-new","request_id":"new","timestamp":"2026-09-20T11:30:00Z","status_code":200}],"total":2,"limit":100,"offset":0}`))
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
	if err != nil || request == nil || request.ID != "new" || request.AuditLogID != "audit-new" || !request.Success {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	if len(offsets) != 1 || offsets[0] != "0" {
		t.Fatalf("offsets=%v", offsets)
	}
}

func TestAuditLogDetailReturnsCompleteEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/audit/detail" || r.URL.Query().Get("log_id") != "req-1" {
			t.Fatalf("request=%s", r.URL)
		}
		_, _ = w.Write([]byte(`{"id":"audit-1","request_id":"req-1","timestamp":"2026-09-20T11:30:00Z","data":{"request_body":{"messages":[{"role":"user","content":"hello"}]}}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	event, err := client.AuditLogDetail(context.Background(), "req-1")
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewReducer().Apply(event)
	if err != nil || request == nil || request.ID != "req-1" || request.AuditLogID != "audit-1" {
		t.Fatalf("request=%+v err=%v", request, err)
	}
}
