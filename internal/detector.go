package internal

import (
	"strings"

	"github.com/tidwall/gjson"
)

// HasWebSearchTool checks if the request payload contains a web_search tool
func HasWebSearchTool(payload []byte) bool {
	tools := gjson.GetBytes(payload, "tools")
	if !tools.IsArray() {
		return false
	}

	for _, tool := range tools.Array() {
		toolType := tool.Get("type").String()
		// Match web_search, web_search_20250305, etc.
		if strings.HasPrefix(toolType, "web_search") {
			return true
		}
	}
	return false
}

// ExtractUserQuery extracts the last user message text for web search
func ExtractUserQuery(payload []byte) string {
	messages := gjson.GetBytes(payload, "messages")
	if !messages.IsArray() {
		return ""
	}

	arr := messages.Array()
	// Find the last user message
	for i := len(arr) - 1; i >= 0; i-- {
		msg := arr[i]
		if msg.Get("role").String() == "user" {
			content := msg.Get("content")

			// String content
			if content.Type == gjson.String {
				return content.String()
			}

			// Array content (multimodal format)
			if content.IsArray() {
				for _, item := range content.Array() {
					if item.Get("type").String() == "text" {
						return item.Get("text").String()
					}
				}
			}
		}
	}
	return ""
}

// IsStreamingRequest checks if the request expects SSE streaming
func IsStreamingRequest(payload []byte) bool {
	return gjson.GetBytes(payload, "stream").Bool()
}

// GetModel extracts the model name from the request
func GetModel(payload []byte) string {
	model := gjson.GetBytes(payload, "model").String()
	if model == "" {
		return "claude-3-5-sonnet-20241022"
	}
	return model
}

// IsClaudeModel checks if the model is a Claude model
func IsClaudeModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "claude")
}
