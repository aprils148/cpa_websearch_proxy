package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/sjson"
)

// ConvertToClaudeSSEStream converts Gemini response to Claude SSE stream events
// Now includes URL resolution and citations support
func ConvertToClaudeSSEStream(ctx context.Context, model string, geminiResp []byte, resolver *URLResolver) []string {
	var events []string

	// Extract data from Gemini response
	textContent := extractTextContent(geminiResp)
	groundingMetadata := extractGroundingMetadata(geminiResp)
	inputTokens := getUsageField(geminiResp, "promptTokenCount")
	outputTokens := getUsageField(geminiResp, "candidatesTokenCount")

	msgID := fmt.Sprintf("msg_%s", uuid.New().String()[:24])
	toolUseID := fmt.Sprintf("srvtoolu_%d", time.Now().UnixNano())

	// Build search query from webSearchQueries
	searchQuery := ""
	if queries := groundingMetadata.Get("webSearchQueries"); queries.IsArray() && len(queries.Array()) > 0 {
		searchQuery = queries.Array()[0].String()
	}

	// 1. message_start
	messageStart := fmt.Sprintf(
		`{"type":"message_start","message":{"id":"%s","type":"message","role":"assistant","content":[],"model":"%s","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":%d,"output_tokens":0}}}`,
		msgID, model, inputTokens)
	events = append(events, "event: message_start\ndata: "+messageStart+"\n\n")

	contentIndex := 0

	// 2. server_tool_use block (index 0)
	serverToolUseStart := fmt.Sprintf(
		`{"type":"content_block_start","index":%d,"content_block":{"type":"server_tool_use","id":"%s","name":"web_search","input":{}}}`,
		contentIndex, toolUseID)
	events = append(events, "event: content_block_start\ndata: "+serverToolUseStart+"\n\n")

	// input_json_delta
	if searchQuery != "" {
		queryJSON, _ := sjson.Set(`{}`, "query", searchQuery)
		inputDelta := fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":""}}`, contentIndex)
		inputDelta, _ = sjson.Set(inputDelta, "delta.partial_json", queryJSON)
		events = append(events, "event: content_block_delta\ndata: "+inputDelta+"\n\n")
	}

	events = append(events, fmt.Sprintf("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", contentIndex))
	contentIndex++

	// 3. web_search_tool_result block (index 1) with resolved URLs
	webSearchResults := extractWebSearchResultsWithResolve(ctx, groundingMetadata, resolver)
	webSearchResultsJSON, _ := json.Marshal(webSearchResults)
	webSearchToolResultStart := fmt.Sprintf(
		`{"type":"content_block_start","index":%d,"content_block":{"type":"web_search_tool_result","tool_use_id":"%s","content":[]}}`,
		contentIndex, toolUseID)
	webSearchToolResultStart, _ = sjson.SetRaw(webSearchToolResultStart, "content_block.content", string(webSearchResultsJSON))
	events = append(events, "event: content_block_start\ndata: "+webSearchToolResultStart+"\n\n")
	events = append(events, fmt.Sprintf("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", contentIndex))
	contentIndex++

	// 4. Citation blocks (NEW) - one block per citation
	groundingSupports := extractGroundingSupports(geminiResp)
	citations := buildCitationsForSSE(groundingSupports, webSearchResults)
	for _, citation := range citations {
		// content_block_start with empty citations array
		citationBlockStart := fmt.Sprintf(
			`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":"","citations":[]}}`,
			contentIndex)
		events = append(events, "event: content_block_start\ndata: "+citationBlockStart+"\n\n")

		// citations_delta with the citation object
		citationObj := map[string]interface{}{
			"type":            citation.Type,
			"cited_text":      citation.CitedText,
			"url":             citation.URL,
			"title":           citation.Title,
			"encrypted_index": citation.EncryptedIndex,
		}
		citationJSON, _ := json.Marshal(citationObj)
		citationDelta := fmt.Sprintf(
			`{"type":"content_block_delta","index":%d,"delta":{"type":"citations_delta","citation":null}}`,
			contentIndex)
		citationDelta, _ = sjson.SetRaw(citationDelta, "delta.citation", string(citationJSON))
		events = append(events, "event: content_block_delta\ndata: "+citationDelta+"\n\n")

		// content_block_stop
		events = append(events, fmt.Sprintf("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", contentIndex))
		contentIndex++
	}

	// 5. text block with Gemini's response
	if textContent != "" {
		textBlockStart := fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, contentIndex)
		events = append(events, "event: content_block_start\ndata: "+textBlockStart+"\n\n")

		// Split text into smaller chunks for more realistic streaming
		// Use rune-based chunking to avoid UTF-8 multi-byte character truncation
		runes := []rune(textContent)
		chunkSize := 50
		for i := 0; i < len(runes); i += chunkSize {
			end := i + chunkSize
			if end > len(runes) {
				end = len(runes)
			}
			chunk := string(runes[i:end])
			textDelta := fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":""}}`, contentIndex)
			textDelta, _ = sjson.Set(textDelta, "delta.text", chunk)
			events = append(events, "event: content_block_delta\ndata: "+textDelta+"\n\n")
		}

		events = append(events, fmt.Sprintf("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", contentIndex))
	}

	// 6. message_delta with stop_reason and usage
	messageDelta := fmt.Sprintf(
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":%d,"output_tokens":%d,"server_tool_use":{"web_search_requests":1}}}`,
		inputTokens, outputTokens)
	events = append(events, "event: message_delta\ndata: "+messageDelta+"\n\n")

	// 7. message_stop
	events = append(events, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	return events
}
