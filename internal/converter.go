package internal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// ConvertToClaudeNonStream converts Gemini response to Claude non-streaming format
// Now includes URL resolution and citations support
func ConvertToClaudeNonStream(ctx context.Context, model string, geminiResp []byte, resolver *URLResolver) string {
	// Extract data from Gemini response
	textContent := extractTextContent(geminiResp)
	groundingMetadata := extractGroundingMetadata(geminiResp)

	// Get usage from Gemini response
	inputTokens := getUsageField(geminiResp, "promptTokenCount")
	outputTokens := getUsageField(geminiResp, "candidatesTokenCount")

	// Generate IDs
	msgID := fmt.Sprintf("msg_%s", uuid.New().String()[:24])
	toolUseID := fmt.Sprintf("srvtoolu_%d", time.Now().UnixNano())

	// Build search query from webSearchQueries
	searchQuery := ""
	if queries := groundingMetadata.Get("webSearchQueries"); queries.IsArray() && len(queries.Array()) > 0 {
		searchQuery = queries.Array()[0].String()
	}

	// Build content array
	content := []map[string]interface{}{}

	// 1. server_tool_use block
	serverToolUse := map[string]interface{}{
		"type":  "server_tool_use",
		"id":    toolUseID,
		"name":  "web_search",
		"input": map[string]interface{}{"query": searchQuery},
	}
	content = append(content, serverToolUse)

	// 2. web_search_tool_result block with resolved URLs
	webSearchResults := extractWebSearchResultsWithResolve(ctx, groundingMetadata, resolver)
	webSearchToolResult := map[string]interface{}{
		"type":        "web_search_tool_result",
		"tool_use_id": toolUseID,
		"content":     webSearchResults,
	}
	content = append(content, webSearchToolResult)

	// 3. Citation text blocks
	groundingSupports := extractGroundingSupports(geminiResp)
	citationBlocks := buildCitationTextBlocks(groundingSupports, webSearchResults)
	content = append(content, citationBlocks...)

	// 4. text block with Gemini's response
	if textContent != "" {
		textBlock := map[string]interface{}{
			"type": "text",
			"text": textContent,
		}
		content = append(content, textBlock)
	}

	// Build final response
	response := map[string]interface{}{
		"id":            msgID,
		"type":          "message",
		"role":          "assistant",
		"content":       content,
		"model":         model,
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]interface{}{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"server_tool_use": map[string]interface{}{
				"web_search_requests": 1,
			},
		},
	}

	respJSON, _ := json.Marshal(response)
	return string(respJSON)
}

// extractTextContent extracts text from Gemini response
func extractTextContent(resp []byte) string {
	// Try wrapped format first (response.candidates...), then top-level (candidates...)
	parts := gjson.GetBytes(resp, "response.candidates.0.content.parts")
	if !parts.IsArray() {
		parts = gjson.GetBytes(resp, "candidates.0.content.parts")
	}

	var text string
	if parts.IsArray() {
		for _, part := range parts.Array() {
			if t := part.Get("text"); t.Exists() {
				text += t.String()
			}
		}
	}
	return text
}

// extractGroundingMetadata extracts grounding metadata from Gemini response
func extractGroundingMetadata(resp []byte) gjson.Result {
	gm := gjson.GetBytes(resp, "response.candidates.0.groundingMetadata")
	if !gm.Exists() {
		gm = gjson.GetBytes(resp, "candidates.0.groundingMetadata")
	}
	return gm
}

// getUsageField extracts a usage field from Gemini response
func getUsageField(resp []byte, field string) int64 {
	val := gjson.GetBytes(resp, "response.usageMetadata."+field).Int()
	if val == 0 {
		val = gjson.GetBytes(resp, "usageMetadata."+field).Int()
	}
	return val
}

// extractWebSearchResultsWithResolve extracts web search results with URL resolution
func extractWebSearchResultsWithResolve(ctx context.Context, gm gjson.Result, resolver *URLResolver) []map[string]interface{} {
	results := extractWebSearchResultsInternal(gm)

	if resolver == nil || len(results) == 0 {
		return results
	}

	// Collect URLs for parallel resolution
	urls := make([]string, len(results))
	for i, result := range results {
		if url, ok := result["url"].(string); ok {
			urls[i] = url
		}
	}

	// Resolve URLs in parallel
	resolvedURLs := resolver.ResolveURLs(ctx, urls)

	// Update results with resolved URLs and regenerate encrypted_content
	for i, result := range results {
		if resolvedURLs[i] != "" && resolvedURLs[i] != urls[i] {
			result["url"] = resolvedURLs[i]
		}
		// Regenerate encrypted_content with resolved URL (use base64 JSON like Antigravity2Api)
		url, _ := result["url"].(string)
		title, _ := result["title"].(string)
		result["encrypted_content"] = generateEncryptedContent(url, title)
	}

	return results
}

// extractWebSearchResultsInternal is the internal implementation
func extractWebSearchResultsInternal(gm gjson.Result) []map[string]interface{} {
	results := []map[string]interface{}{}

	chunks := gm.Get("groundingChunks")
	if !chunks.IsArray() {
		return results
	}

	for _, chunk := range chunks.Array() {
		web := chunk.Get("web")
		if !web.Exists() {
			continue
		}

		result := map[string]interface{}{
			"type":     "web_search_result",
			"page_age": nil,
		}

		title := ""
		url := ""

		if t := web.Get("title"); t.Exists() {
			title = t.String()
			result["title"] = title
		}
		if uri := web.Get("uri"); uri.Exists() {
			url = uri.String()
			result["url"] = url
		}

		// Generate encrypted_content as base64 JSON (matching Antigravity2Api format)
		result["encrypted_content"] = generateEncryptedContent(url, title)

		results = append(results, result)
	}

	return results
}

// generateEncryptedContent creates base64-encoded JSON for encrypted_content field
func generateEncryptedContent(url, title string) string {
	payload := map[string]string{
		"url":   url,
		"title": title,
	}
	payloadJSON, _ := json.Marshal(payload)
	return base64.StdEncoding.EncodeToString(payloadJSON)
}
