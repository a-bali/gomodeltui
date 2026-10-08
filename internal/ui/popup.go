package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/a-bali/gomodeltui/internal/gomodel"
	"github.com/charmbracelet/lipgloss"
)

func buildPopupLines(raw string) []string {
	var root any
	if json.Unmarshal([]byte(raw), &root) != nil {
		return strings.Split(raw, "\n")
	}
	var lines []string
	lines = append(lines, "REQUEST")
	for _, field := range []string{"request_id", "type", "timestamp", "user_path", "session_id", "endpoint", "requested_model", "provider_name", "resolved_model", "status_code", "duration_ns", "input_tokens", "output_tokens", "cached_input_ratio", "error"} {
		if value, ok := findJSONValue(root, field); ok && !isContainer(value) {
			lines = append(lines, fmt.Sprintf("  %-20s %v", field, displayJSONValue(value)))
		}
	}

	if attempts, ok := findJSONValue(root, "attempts"); ok {
		if values, ok := attempts.([]any); ok && len(values) > 0 {
			lines = append(lines, "", fmt.Sprintf("ROUTING ATTEMPTS (%d)", len(values)))
			for index, value := range values {
				item, ok := value.(map[string]any)
				if !ok {
					continue
				}
				provider := firstJSONString(item, "provider_name", "provider", "provider_type")
				model := firstJSONString(item, "model")
				status := firstJSONString(item, "status_code", "status")
				result := "error"
				if success, ok := item["success"].(bool); ok && success {
					result = "success"
				}
				lines = append(lines, fmt.Sprintf("  %d  %s/%s  %s  %s", index+1, provider, model, status, result))
				if result == "error" {
					lines = append(lines,
						"      error_type: "+firstJSONString(item, "error_type"),
						"      error_code: "+firstJSONString(item, "error_code"),
						"      error_message: "+firstJSONString(item, "error_message"),
					)
				}
			}
		}
	}

	if body, ok := findJSONValue(root, "request_body"); ok {
		if messages, ok := bodyMessages(body); ok {
			lines = append(lines, "", fmt.Sprintf("MESSAGES (%d)", len(messages)))
			for index, message := range messages {
				lines = append(lines, formatMessage(index+1, message)...)
			}
		}
	}
	if body, ok := findJSONValue(root, "response_body"); ok {
		lines = append(lines, "", "RESPONSE")
		lines = append(lines, formatBodySummary(body)...)
	}
	if len(lines) == 1 {
		lines = append(lines, "  No structured request body found.")
	}
	return lines
}

type popupMessage struct {
	role     string
	lines    []string
	expanded bool
}

func buildPopupSummaryLines(raw string) []string {
	lines := buildPopupLines(raw)
	messageIndex := -1
	for index, line := range lines {
		if strings.HasPrefix(line, "MESSAGES (") {
			messageIndex = index
			break
		}
	}
	if messageIndex >= 0 {
		for index := messageIndex; index < len(lines); index++ {
			if lines[index] == "RESPONSE" {
				return append(append(append([]string{}, lines[:messageIndex]...), ""), lines[index:]...)
			}
		}
		return append(append([]string{}, lines[:messageIndex]...), "")
	}
	return lines
}

func parsePopupMessages(raw string) []popupMessage {
	var root any
	if json.Unmarshal([]byte(raw), &root) != nil {
		return nil
	}
	body, ok := findJSONValue(root, "request_body")
	if !ok {
		return nil
	}
	messages, ok := bodyMessages(body)
	if !ok {
		return nil
	}
	result := make([]popupMessage, 0, len(messages))
	for index, value := range messages {
		formatted := formatMessage(index+1, value)
		role := "unknown"
		if item, ok := value.(map[string]any); ok {
			role = firstJSONString(item, "role", "type")
		}
		if len(formatted) > 0 {
			formatted = formatted[1:]
		}
		result = append(result, popupMessage{role: role, lines: formatted, expanded: index == len(messages)-1})
	}
	return result
}

func popupHasResponse(raw string) bool {
	var root any
	if json.Unmarshal([]byte(raw), &root) != nil {
		return false
	}
	value, ok := findJSONValue(root, "response_body")
	return ok && value != nil
}

func popupResponseState(raw string) (hasResponse, final bool) {
	var root any
	if json.Unmarshal([]byte(raw), &root) != nil {
		return false, false
	}
	response, ok := findJSONValue(root, "response_body")
	if !ok || response == nil {
		return false, false
	}
	lifecycle, _ := findJSONValue(root, "type")
	typeName, _ := lifecycle.(string)
	return true, strings.HasSuffix(typeName, ".completed") || strings.HasSuffix(typeName, ".failed")
}

func popupRoleStyle(role string) lipgloss.Style {
	switch strings.ToLower(role) {
	case "system":
		return popupSystemStyle
	case "developer":
		return popupDeveloperStyle
	case "user":
		return popupUserStyle
	case "assistant":
		return popupAssistantStyle
	case "tool", "tool_result", "function", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
		return popupToolStyle
	case "reasoning":
		return popupAssistantStyle
	default:
		return mutedStyle
	}
}

func findJSONValue(value any, key string) (any, bool) {
	switch item := value.(type) {
	case map[string]any:
		if found, ok := item[key]; ok {
			return found, true
		}
		for _, child := range item {
			if found, ok := findJSONValue(child, key); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range item {
			if found, ok := findJSONValue(child, key); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func firstJSONString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			return displayJSONValue(value)
		}
	}
	return "-"
}

func displayJSONValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

func isContainer(value any) bool {
	switch value.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

func bodyMessages(body any) ([]any, bool) {
	return gomodel.BodyMessages(body)
}

func formatMessage(index int, value any) []string {
	item, ok := value.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("  [%d] %s", index, displayJSONValue(value))}
	}
	role := firstJSONString(item, "role", "type")
	lines := []string{fmt.Sprintf("  [%d] %s", index, role)}
	switch item["type"] {
	case "function_call", "custom_tool_call":
		lines = append(lines, "      tool call: "+firstJSONString(item, "name"))
		lines = appendIndented(lines, firstJSONString(item, "arguments", "input"), "        args: ")
	case "function_call_output", "custom_tool_call_output":
		if text, ok := item["output"].(string); ok {
			lines = append(lines, formatToolResult(text)...)
		} else if output, ok := item["output"]; ok {
			lines = append(lines, formatContentBlocks(output)...)
		}
	case "reasoning":
		if summary, ok := item["summary"]; ok {
			lines = append(lines, formatContentBlocks(summary)...)
		}
	}
	if callID, ok := item["call_id"]; ok {
		lines = append(lines, "      call_id: "+displayJSONValue(callID))
	}
	if calls, ok := item["tool_calls"].([]any); ok {
		for _, call := range calls {
			lines = append(lines, formatToolCall(call)...)
		}
	}
	if content, ok := item["content"]; ok {
		if text, ok := content.(string); ok {
			if strings.EqualFold(role, "tool") {
				lines = append(lines, formatToolResult(text)...)
				return lines
			}
			if function := functionName(text); function != "" {
				lines = append(lines, "      function: "+function)
			} else {
				lines = appendIndented(lines, text, "      ")
			}
		} else {
			lines = append(lines, formatContentBlocks(content)...)
		}
	}
	for _, key := range []string{"thinking", "reasoning_content", "reasoning"} {
		if thought, ok := item[key].(string); ok && thought != "" {
			lines = append(lines, "      thinking:")
			lines = appendIndented(lines, thought, "        ")
		}
	}
	return lines
}

func formatToolResult(text string) []string {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &value) == nil {
		if item, ok := value.(map[string]any); ok {
			if exitCode, exists := item["exit_code"]; exists {
				lines := []string{"      exit_code: " + displayJSONValue(exitCode)}
				if output, exists := item["output"]; exists {
					return appendIndented(lines, displayJSONValue(output), "      output: ")
				}
				return lines
			}
		}
	}
	return appendIndented(nil, text, "      output: ")
}

func formatToolCall(value any) []string {
	item, ok := value.(map[string]any)
	if !ok {
		return []string{"      tool call: " + displayJSONValue(value)}
	}
	function, _ := item["function"].(map[string]any)
	name := firstJSONString(function, "name")
	arguments := firstJSONString(function, "arguments")
	return []string{"      tool call: " + name, "        args: " + arguments}
}

func formatContentBlocks(value any) []string {
	blocks, ok := value.([]any)
	if !ok {
		return []string{"      content: " + displayJSONValue(value)}
	}
	var lines []string
	for _, block := range blocks {
		item, ok := block.(map[string]any)
		if !ok {
			lines = append(lines, "      "+displayJSONValue(block))
			continue
		}
		typ := firstJSONString(item, "type")
		if text, ok := item["text"].(string); ok {
			lines = append(lines, "      "+typ+":")
			lines = appendIndented(lines, text, "        ")
		} else if function, ok := item["function"].(map[string]any); ok {
			lines = append(lines, "      "+typ+": "+firstJSONString(function, "name"))
		} else {
			lines = append(lines, "      "+typ+": "+displayJSONValue(item))
		}
	}
	return lines
}

func formatBodySummary(body any) []string {
	if item, ok := body.(map[string]any); ok {
		if output, ok := item["output"].([]any); ok && len(output) > 0 {
			var lines []string
			for index, value := range output {
				lines = append(lines, formatMessage(index+1, value)...)
			}
			return lines
		}
		if text, ok := item["output_text"].(string); ok && text != "" {
			return appendIndented(nil, text, "  ")
		}
		if choices, ok := item["choices"].([]any); ok {
			var lines []string
			for _, choice := range choices {
				if value, ok := findJSONValue(choice, "content"); ok {
					lines = appendIndented(lines, displayJSONValue(value), "  ")
				}
			}
			if len(lines) > 0 {
				return lines
			}
		}
	}
	return []string{"  " + displayJSONValue(body)}
}

func appendIndented(lines []string, text, prefix string) []string {
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, prefix+line)
	}
	return lines
}

func functionName(text string) string {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &value) != nil {
		return ""
	}
	if item, ok := value.(map[string]any); ok {
		if function, ok := item["function"].(map[string]any); ok {
			if name, ok := function["name"].(string); ok {
				return name
			}
		}
		if name, ok := item["name"].(string); ok {
			return name
		}
	}
	return ""
}
