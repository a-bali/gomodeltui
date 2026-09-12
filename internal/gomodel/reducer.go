package gomodel

import (
	"bytes"
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
	Failover     bool
	LastTurn     string
	Attempts     []Attempt
	RawJSON      string
}

type Attempt struct {
	Seq          int
	ProviderType string
	ProviderName string
	Model        string
	StatusCode   int
	Success      bool
	ErrorType    string
	ErrorCode    string
	ErrorMessage string
	StartedAt    time.Time
	Duration     time.Duration
}

func (r Request) LogRows() []Request {
	if !r.Terminal || len(r.Attempts) <= 1 {
		return []Request{r}
	}
	collapsed := make([]Attempt, 0, len(r.Attempts))
	for _, attempt := range r.Attempts {
		if len(collapsed) > 0 && !attempt.Success && !collapsed[len(collapsed)-1].Success && attemptKey(attempt) == attemptKey(collapsed[len(collapsed)-1]) {
			collapsed[len(collapsed)-1] = attempt
			continue
		}
		collapsed = append(collapsed, attempt)
	}
	rows := make([]Request, 0, len(collapsed))
	for index, attempt := range collapsed {
		provider := attempt.ProviderName
		if provider == "" {
			provider = attempt.ProviderType
		}
		status := ""
		if attempt.StatusCode != 0 {
			status = strconv.Itoa(attempt.StatusCode)
		}
		errorText := firstNonEmpty(attempt.ErrorMessage, attempt.ErrorCode, attempt.ErrorType)
		row := r
		row.ID = fmt.Sprintf("%s/attempt-%d", r.ID, attempt.Seq)
		row.Timestamp = attempt.StartedAt
		if row.Timestamp.IsZero() {
			row.Timestamp = r.Timestamp
		}
		row.Provider = provider
		row.Model = canonicalModel(attempt.Model, provider)
		row.RoutedModel = routedModel(attempt.Model, provider)
		row.StatusCode = status
		row.Error = errorText
		row.Duration = attempt.Duration
		row.Success = attempt.Success
		row.Failover = index > 0
		row.LastTurn = ""
		rows = append(rows, row)
	}
	// Usage and prompt details belong on the final routed response row.
	rows[len(rows)-1].ID = r.ID
	rows[len(rows)-1].InputTokens = r.InputTokens
	rows[len(rows)-1].CacheRatio = r.CacheRatio
	rows[len(rows)-1].OutputTokens = r.OutputTokens
	rows[len(rows)-1].LastTurn = r.LastTurn
	return rows
}

func attemptKey(attempt Attempt) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", attempt.ProviderName, attempt.ProviderType, attempt.StatusCode, attempt.ErrorType, attempt.ErrorCode)
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
	if strings.HasPrefix(firstString(event.Event, payload.Type), "audit.") {
		request.RawJSON = prettyJSON(event.Data)
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
	if auditData, ok := fields["data"].(map[string]any); ok {
		if attempts, ok := auditData["attempts"].([]any); ok && len(attempts) > 1 {
			request.Failover = true
			request.Attempts = parseAttempts(attempts)
		}
	}

	eventType := firstString(event.Event, payload.Type)
	if strings.HasPrefix(eventType, "audit.") && (strings.HasSuffix(eventType, ".completed") || strings.HasSuffix(eventType, ".failed")) {
		request.Terminal = true
		// A stream can establish an HTTP 200 response and fail later while
		// reading or delivering SSE data. In that case the status remains 200,
		// but the audit error is still a failed request.
		request.Success = isSuccessStatus(request.StatusCode) && !(strings.HasSuffix(eventType, ".failed") && request.Error != "")
		if request.Success {
			request.Error = ""
		}
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

func parseAttempts(values []any) []Attempt {
	attempts := make([]Attempt, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		attempts = append(attempts, Attempt{
			Seq: intValue(item["seq"]), ProviderType: firstString(item["provider_type"]), ProviderName: firstString(item["provider_name"]), Model: firstString(item["model"]), StatusCode: intValue(item["status_code"]), Success: boolValue(item["success"]), ErrorType: firstString(item["error_type"]), ErrorCode: firstString(item["error_code"]), ErrorMessage: firstString(item["error_message"]), StartedAt: parseTime(firstString(item["started_at"])), Duration: parseDuration(item["duration_ns"]),
		})
	}
	return attempts
}

func intValue(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	if text, ok := value.(string); ok {
		number, _ := strconv.Atoi(text)
		return number
	}
	return 0
}
func boolValue(value any) bool { result, _ := value.(bool); return result }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func prettyJSON(raw []byte) string {
	var formatted bytes.Buffer
	if json.Indent(&formatted, raw, "", "  ") != nil {
		return string(raw)
	}
	return formatted.String()
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

func isSuccessStatus(status string) bool {
	code, err := strconv.Atoi(status)
	return err == nil && code >= 200 && code < 300
}
