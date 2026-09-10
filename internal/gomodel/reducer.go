package gomodel

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Request struct {
	ID           string
	Timestamp    time.Time
	UserPath     string
	Model        string
	Provider     string
	Endpoint     string
	StatusCode   string
	InputTokens  int
	OutputTokens int
	Error        string
	Duration     time.Duration
	Terminal     bool
	Success      bool
}

func (r Request) TimestampOrNow() time.Time {
	if r.Timestamp.IsZero() {
		return time.Now()
	}
	return r.Timestamp
}

type Reducer struct {
	requests map[string]*Request
}

func NewReducer() *Reducer { return &Reducer{requests: make(map[string]*Request)} }

func (r *Reducer) Apply(event Event) (*Request, error) {
	if event.Event == "heartbeat" || event.Event == "" || event.ID == "" && len(event.Data) == 0 {
		return nil, nil
	}
	var payload struct {
		RequestID string          `json:"request_id"`
		Timestamp string          `json:"timestamp"`
		Type      string          `json:"type"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, fmt.Errorf("decode live event: %w", err)
	}
	requestID := payload.RequestID
	if requestID == "" {
		return nil, nil
	}
	fields := map[string]any{}
	if len(payload.Data) > 0 && string(payload.Data) != "null" {
		if err := json.Unmarshal(payload.Data, &fields); err != nil {
			return nil, fmt.Errorf("decode live event data: %w", err)
		}
	}
	request := r.requests[requestID]
	if request == nil {
		request = &Request{ID: requestID}
		r.requests[requestID] = request
	}
	if timestamp := parseTime(firstString(payload.Timestamp, fields["timestamp"])); !timestamp.IsZero() {
		request.Timestamp = timestamp
	}
	if value := firstString(fields["user_path"]); value != "" {
		request.UserPath = value
	}
	if value := firstString(fields["resolved_model"], fields["requested_model"], fields["model"]); value != "" {
		request.Model = value
	}
	if value := firstString(fields["provider_name"], fields["provider"]); value != "" {
		request.Provider = value
	}
	if value := firstString(fields["path"], fields["endpoint"]); value != "" {
		request.Endpoint = value
	}
	if value := firstString(fields["status_code"], fields["status"]); value != "" {
		request.StatusCode = value
	}
	if value := firstInt(fields["input_tokens"]); value > 0 {
		request.InputTokens = value
	}
	if value := firstInt(fields["output_tokens"]); value > 0 {
		request.OutputTokens = value
	}
	if value := firstString(fields["error"], fields["error_type"]); value != "" {
		request.Error = value
	}
	if value := parseDuration(fields["duration_ns"], fields["duration_ms"]); value > 0 {
		request.Duration = value
	}

	eventType := firstString(payload.Type, event.Event)
	if strings.HasPrefix(eventType, "audit.") && (strings.HasSuffix(eventType, ".completed") || strings.HasSuffix(eventType, ".failed")) {
		request.Terminal = true
		request.Success = request.Error == "" && !isErrorStatus(request.StatusCode)
	}
	copy := *request
	return &copy, nil
}

func firstInt(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	if text, ok := value.(string); ok {
		number, _ := strconv.Atoi(text)
		return number
	}
	return 0
}

func firstString(values ...any) string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return v
			}
		case float64:
			return strconv.FormatInt(int64(v), 10)

		}
	}
	return ""
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func parseDuration(values ...any) time.Duration {
	for index, value := range values {
		if number, ok := value.(float64); ok {
			if index == 1 {
				return time.Duration(number) * time.Millisecond
			}
			return time.Duration(number)
		}
	}
	return 0
}

func isErrorStatus(status string) bool {
	if code, err := strconv.Atoi(status); err == nil {
		return code >= 400
	}
	return false
}
