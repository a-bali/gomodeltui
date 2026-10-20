package gomodel

import (
	"encoding/json"
	"strings"
)

func (r Request) IsMCP() bool {
	return strings.EqualFold(r.Provider, "mcp") || r.Endpoint == "/mcp" || strings.HasPrefix(r.Endpoint, "/mcp/")
}

// MCPBodies reads the audit envelope explicitly: nested tool data may contain
// arbitrary keys, including request_body, method, and messages.
func MCPBodies(raw string) (map[string]any, map[string]any) {
	var envelope struct {
		Data struct {
			Data struct {
				Request  json.RawMessage `json:"request_body"`
				Response json.RawMessage `json:"response_body"`
			} `json:"data"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil {
		return nil, nil
	}
	return decodeMCPBody(envelope.Data.Data.Request), decodeMCPBody(envelope.Data.Data.Response)
}

func decodeMCPBody(raw json.RawMessage) map[string]any {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		raw = json.RawMessage(text)
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return body
}

func (r Request) MCPAction() string {
	if r.MCPActionLabel != "" {
		return r.MCPActionLabel
	}
	body, _ := MCPBodies(r.RawJSON)
	method, _ := body["method"].(string)
	if method == "" {
		method = r.ClientModel
	}
	params, _ := body["params"].(map[string]any)
	if name, _ := params["name"].(string); name != "" {
		method += " " + name
	}
	if method == "" {
		method = r.Endpoint
	}
	return method
}

func (r *Request) captureMCP(raw string) {
	body, response := MCPBodies(raw)
	method, _ := body["method"].(string)
	params, _ := body["params"].(map[string]any)
	if name, _ := params["name"].(string); name != "" {
		method += " " + name
	}
	if strings.TrimSpace(method) != "" {
		r.MCPActionLabel = strings.TrimSpace(method)
	}
	if response != nil {
		result, _ := response["result"].(map[string]any)
		toolError, _ := result["isError"].(bool)
		r.MCPResultKnown, r.MCPFailed = true, response["error"] != nil || toolError
	}
}
