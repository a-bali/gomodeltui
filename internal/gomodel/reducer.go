package gomodel

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Request struct {
	ID        string
	Timestamp time.Time
	Model     string
	Provider  string
	Endpoint  string
	Status    string
	Error     string
	Duration  time.Duration
	Terminal  bool
	Success   bool
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
	request.Timestamp = parseTime(firstString(payload.Timestamp, fields["timestamp"]))
	request.Model = firstString(fields["resolved_model"], fields["requested_model"], fields["model"])
	request.Provider = firstString(fields["provider_name"], fields["provider"])
	request.Endpoint = firstString(fields["path"], fields["endpoint"])
	request.Status = firstString(fields["status_code"], fields["status"])
	request.Error = firstString(fields["error"], fields["error_type"])
	request.Duration = parseDuration(fields["duration_ns"], fields["duration_ms"])

	eventType := firstString(payload.Type, event.Event)
	if strings.HasSuffix(eventType, ".completed") || strings.HasSuffix(eventType, ".failed") {
		request.Terminal = true
		request.Success = request.Error == "" && !isErrorStatus(request.Status)
	}
	copy := *request
	return &copy, nil
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
	for _, value := range values {
		if number, ok := value.(float64); ok {
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
