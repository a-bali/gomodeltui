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
	SessionID    string
	ClientModel  string
	RoutedModel  string
	Model        string
	Provider     string
	Endpoint     string
	StatusCode   string
	InputTokens  int
	CacheRatio   float64
	OutputTokens int
	Error        string
	Duration     time.Duration
	Terminal     bool
	Success      bool
	LastTurn     string
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
	if value := firstString(fields["session_id"]); value != "" {
		request.SessionID = value
	}
	if value := firstString(fields["provider_name"], fields["provider"]); value != "" {
		request.Provider = value
	}
	if value := firstString(fields["resolved_model"], fields["requested_model"], fields["model"]); value != "" {
		if requested := firstString(fields["requested_model"]); requested != "" {
			request.ClientModel = requested
		}
		request.RoutedModel = routedModel(value, request.Provider)
		request.Model = canonicalModel(value, request.Provider)
	}
	if value := firstString(fields["requested_model"]); value != "" {
		request.ClientModel = value
	}
	if value, ok := firstFloat(fields["cached_input_ratio"]); ok {
		request.CacheRatio = value
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
	if value := lastTurn(fields["data"]); value != "" {
		request.LastTurn = value
	}

	eventType := firstString(event.Event, payload.Type)
	if strings.HasPrefix(eventType, "audit.") && (strings.HasSuffix(eventType, ".completed") || strings.HasSuffix(eventType, ".failed")) {
		request.Terminal = true
		request.Success = request.Error == "" && !isErrorStatus(request.StatusCode)
	}
	copy := *request
	return &copy, nil
}

func canonicalModel(model, provider string) string {
	prefix := strings.TrimSpace(provider) + "/"
	if provider != "" && strings.HasPrefix(model, prefix) {
		return strings.TrimPrefix(model, prefix)
	}
	return model
}

func routedModel(model, provider string) string {
	model = canonicalModel(model, provider)
	if provider == "" {
		return model
	}
	return provider + "/" + model
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

func firstFloat(value any) (float64, bool) {
	if number, ok := value.(float64); ok {
		return number, true
	}
	if text, ok := value.(string); ok {
		number, err := strconv.ParseFloat(text, 64)
		return number, err == nil
	}
	return 0, false
}

func lastTurn(value any) string {
	auditData, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	body, ok := auditData["request_body"].(map[string]any)
	if !ok {
		return ""
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return ""
	}
	last, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		return ""
	}
	content := last["content"]
	if text, ok := content.(string); ok {
		if function := jsonFunction(text); function != "" {
			return function
		}
		return text
	}
	if function := jsonFunctionValue(content); function != "" {
		return function
	}
	return ""
}

func jsonFunction(text string) string {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &value) != nil {
		return ""
	}
	return jsonFunctionValue(value)
}

func jsonFunctionValue(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"function", "name", "tool_name"} {
		if name, ok := object[key].(string); ok && name != "" {
			return name
		}
	}
	if function, ok := object["function"].(map[string]any); ok {
		if name, ok := function["name"].(string); ok {
			return name
		}
	}
	return ""
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
