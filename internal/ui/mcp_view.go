package ui

import (
	"encoding/json"
	"fmt"
	"github.com/balia/gomodeltui/internal/gomodel"
	"time"
)

func buildMCPPopupLines(request gomodel.Request) []string {
	lines := []string{"REQUEST", "  protocol             MCP / JSON-RPC", "  request_id           " + request.ID,
		"  timestamp            " + request.Timestamp.Format(time.RFC3339Nano),
		"  action               " + request.MCPAction(), "  user_path            " + request.UserPath,
		"  endpoint             " + request.Endpoint, "  status_code          " + request.StatusCode,
		"  duration             " + request.Duration.String()}
	if request.Error != "" {
		lines = append(lines, "  error                "+request.Error)
	}
	body, response := gomodel.MCPBodies(request.RawJSON)
	if body != nil {
		for _, key := range []string{"jsonrpc", "id", "method"} {
			if value, ok := body[key]; ok {
				lines = append(lines, fmt.Sprintf("  %-20s %v", key, value))
			}
		}
		if params, ok := body["params"]; ok {
			lines = appendMCPSection(lines, "PARAMETERS / ARGUMENTS", params)
		}
	} else {
		lines = append(lines, "  Request body unavailable.")
	}
	if response != nil {
		// Preserve every result field, including content blocks, structuredContent,
		// isError and JSON-RPC errors, instead of interpreting these as chat choices.
		lines = appendMCPSection(lines, "RESPONSE", response)
	} else if _, hasID := body["id"]; body != nil && !hasID {
		lines = append(lines, "", "RESPONSE", "  Notification — no response expected.")
	} else {
		lines = append(lines, "", "RESPONSE", "  Response body unavailable.")
	}
	return lines
}

func appendMCPSection(lines []string, title string, value any) []string {
	lines = append(lines, "", title)
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return append(lines, "  Could not format JSON.")
	}
	return appendIndented(lines, string(encoded), "    ")
}
