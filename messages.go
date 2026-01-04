package main

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

// GeminiContent represents a single content entry in Gemini format
type GeminiContent struct {
	Role  string       `json:"role"`
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart represents a part within a Gemini content entry
type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

// GeminiFunctionCall represents a function call in Gemini format
type GeminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
	ID   string                 `json:"id,omitempty"`
}

// GeminiFunctionResponse represents a function response in Gemini format
type GeminiFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
	ID       string                 `json:"id,omitempty"`
}

// TransformMessages converts Claude messages to Gemini contents format
// Returns the transformed contents array ready for Gemini API
func TransformMessages(claudePayload []byte) ([]GeminiContent, error) {
	messages := gjson.GetBytes(claudePayload, "messages")
	if !messages.IsArray() {
		return nil, nil
	}

	// Pre-scan to build tool_use id -> name mapping
	toolIdToName := buildToolIdToNameMap(messages)

	var contents []GeminiContent

	for _, msg := range messages.Array() {
		role := msg.Get("role").String()

		// Map Claude roles to Gemini roles
		geminiRole := role
		if role == "assistant" {
			geminiRole = "model"
		}

		content := GeminiContent{
			Role:  geminiRole,
			Parts: []GeminiPart{},
		}

		// Handle content - can be string or array
		msgContent := msg.Get("content")

		if msgContent.Type == gjson.String {
			// Simple string content
			text := msgContent.String()
			if text != "" {
				content.Parts = append(content.Parts, GeminiPart{Text: text})
			}
		} else if msgContent.IsArray() {
			// Array of content blocks
			for _, item := range msgContent.Array() {
				parts := transformContentBlock(item, toolIdToName)
				content.Parts = append(content.Parts, parts...)
			}
		}

		// Only add if has parts
		if len(content.Parts) > 0 {
			contents = append(contents, content)
		}
	}

	return contents, nil
}

// transformContentBlock transforms a single Claude content block to Gemini parts
func transformContentBlock(block gjson.Result, toolIdToName map[string]string) []GeminiPart {
	var parts []GeminiPart

	blockType := block.Get("type").String()

	switch blockType {
	case "text":
		text := block.Get("text").String()
		if text != "" && text != "(no content)" {
			parts = append(parts, GeminiPart{Text: text})
		}

	case "tool_use":
		// Convert to Gemini functionCall
		name := block.Get("name").String()
		id := block.Get("id").String()

		var args map[string]interface{}
		inputRaw := block.Get("input").Raw
		if inputRaw != "" {
			_ = json.Unmarshal([]byte(inputRaw), &args)
		}

		fc := &GeminiFunctionCall{
			Name: name,
			Args: args,
			ID:   id,
		}
		parts = append(parts, GeminiPart{FunctionCall: fc})

	case "tool_result":
		// Convert to Gemini functionResponse
		toolUseId := block.Get("tool_use_id").String()

		// Get function name from mapping
		funcName := toolIdToName[toolUseId]
		if funcName == "" {
			funcName = toolUseId // Fallback to ID if name not found
		}

		// Extract content
		var resultContent string
		content := block.Get("content")
		if content.Type == gjson.String {
			resultContent = content.String()
		} else if content.IsArray() {
			// Array format - concatenate text items
			for _, item := range content.Array() {
				if item.Get("type").String() == "text" {
					resultContent += item.Get("text").String()
				}
			}
		}

		fr := &GeminiFunctionResponse{
			Name: funcName,
			Response: map[string]interface{}{
				"result": resultContent,
			},
			ID: toolUseId,
		}
		parts = append(parts, GeminiPart{FunctionResponse: fr})

	case "thinking", "redacted_thinking":
		// Skip thinking blocks as per design decision
		// Do nothing

	case "image":
		// Skip images as per design decision (not supported yet)
		// Do nothing
	}

	return parts
}

// buildToolIdToNameMap scans messages to build a mapping of tool_use IDs to function names
func buildToolIdToNameMap(messages gjson.Result) map[string]string {
	mapping := make(map[string]string)

	if !messages.IsArray() {
		return mapping
	}

	for _, msg := range messages.Array() {
		content := msg.Get("content")
		if !content.IsArray() {
			continue
		}

		for _, item := range content.Array() {
			if item.Get("type").String() == "tool_use" {
				id := item.Get("id").String()
				name := item.Get("name").String()
				if id != "" && name != "" {
					mapping[id] = name
				}
			}
		}
	}

	return mapping
}
