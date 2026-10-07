package gomodel

import "strings"

// BodyMessages returns conversation items from chat or Responses request bodies.
// Responses instructions are shown as a separate system message.
func BodyMessages(body any) ([]any, bool) {
	item, ok := body.(map[string]any)
	if !ok {
		return nil, false
	}
	if messages, ok := item["messages"].([]any); ok {
		return messages, true
	}
	var messages []any
	switch input := item["input"].(type) {
	case string:
		messages = []any{map[string]any{"role": "user", "content": input}}
	case []any:
		messages = append(messages, input...)
	default:
		return nil, false
	}
	if instructions, ok := item["instructions"].(string); ok && instructions != "" {
		messages = append([]any{map[string]any{"role": "system", "content": instructions}}, messages...)
	}
	return messages, true
}

func responseItemPreview(value any) string {
	item, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	switch item["type"] {
	case "function_call", "custom_tool_call":
		name, _ := item["name"].(string)
		if name == "" {
			return ""
		}
		arguments := firstString(item["arguments"], item["input"])
		return strings.TrimSpace("tool: " + name + " " + arguments)
	case "function_call_output", "custom_tool_call_output":
		return responseContentText(item["output"])
	case "reasoning":
		return ""
	}
	if text := responseContentText(item["content"]); text != "" {
		if function := jsonFunction(text); function != "" {
			return function
		}
		return text
	}
	if function := jsonFunctionValue(item["content"]); function != "" {
		return function
	}
	return responseToolCalls(item["tool_calls"])
}
