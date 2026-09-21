package gomodel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("GoModel URL is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("GoModel token is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, token: token, http: httpClient}, nil
}

func (c *Client) do(ctx context.Context, path string, headers http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("GoModel %s returned HTTP %d", path, resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) Health(ctx context.Context) error {
	resp, err := c.do(ctx, "/health/ready", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

func (c *Client) LiveLogs(ctx context.Context, lastEventID string) (*http.Response, error) {
	headers := http.Header{"Accept": []string{"text/event-stream"}}
	if strings.TrimSpace(lastEventID) != "" {
		headers.Set("Last-Event-ID", lastEventID)
	}
	return c.do(ctx, "/admin/live/logs", headers)
}

// AuditLogs returns persisted, completed audit entries at or after start.
// GoModel's date filters operate on whole days, so timestamps are filtered
// locally as well to honour the caller's exact retention window.
func (c *Client) AuditLogs(ctx context.Context, start time.Time) ([]Event, error) {
	query := url.Values{
		"start_date": {start.Format("2006-01-02")},
		"end_date":   {time.Now().Format("2006-01-02")},
		"limit":      {"100"},
	}
	var events []Event
	for offset := 0; ; {
		query.Set("offset", strconv.Itoa(offset))
		resp, err := c.do(ctx, "/admin/audit/log?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Entries []json.RawMessage `json:"entries"`
			Total   int               `json:"total"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode GoModel audit log: %w", err)
		}
		for _, entry := range page.Entries {
			event, timestamp, err := auditLogEvent(entry)
			if err != nil {
				return nil, err
			}
			if !timestamp.Before(start) {
				events = append(events, event)
			}
		}
		offset += len(page.Entries)
		if len(page.Entries) == 0 || offset >= page.Total {
			return events, nil
		}
	}
}

func auditLogEvent(entry json.RawMessage) (Event, time.Time, error) {
	var header struct {
		RequestID string    `json:"request_id"`
		Timestamp time.Time `json:"timestamp"`
	}
	if err := json.Unmarshal(entry, &header); err != nil {
		return Event{}, time.Time{}, fmt.Errorf("decode GoModel audit entry: %w", err)
	}
	payload, err := json.Marshal(struct {
		RequestID string          `json:"request_id"`
		Timestamp string          `json:"timestamp"`
		Type      string          `json:"type"`
		Data      json.RawMessage `json:"data"`
	}{header.RequestID, header.Timestamp.Format(time.RFC3339Nano), "audit.completed", entry})
	if err != nil {
		return Event{}, time.Time{}, fmt.Errorf("encode GoModel audit entry: %w", err)
	}
	return Event{Event: "audit.completed", Data: payload}, header.Timestamp, nil
}
