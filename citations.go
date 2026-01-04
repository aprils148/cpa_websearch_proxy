package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/tidwall/gjson"
)

// Citation represents a Claude citation block
type Citation struct {
	Type           string `json:"type"`
	CitedText      string `json:"cited_text"`
	URL            string `json:"url"`
	Title          string `json:"title"`
	EncryptedIndex string `json:"encrypted_index"`
}

// extractGroundingSupports extracts grounding supports from Gemini response
// Tries multiple possible paths in the response structure
func extractGroundingSupports(resp []byte) gjson.Result {
	// Try wrapped format first (response.candidates...)
	gs := gjson.GetBytes(resp, "response.candidates.0.groundingSupports")
	if gs.IsArray() {
		return gs
	}

	// Try direct candidates path
	gs = gjson.GetBytes(resp, "candidates.0.groundingSupports")
	if gs.IsArray() {
		return gs
	}

	// Try inside groundingMetadata
	gs = gjson.GetBytes(resp, "response.candidates.0.groundingMetadata.groundingSupports")
	if gs.IsArray() {
		return gs
	}

	gs = gjson.GetBytes(resp, "candidates.0.groundingMetadata.groundingSupports")
	return gs
}

// buildCitation creates a Claude citation from a Gemini grounding support
// Returns nil if the support is invalid or missing required data
func buildCitation(support gjson.Result, results []map[string]interface{}) *Citation {
	// Extract cited text from segment
	citedText := support.Get("segment.text").String()
	if citedText == "" {
		return nil
	}

	// Get grounding chunk index
	indices := support.Get("groundingChunkIndices").Array()
	if len(indices) == 0 {
		return nil
	}

	idx := int(indices[0].Int())
	if idx < 0 || idx >= len(results) {
		return nil
	}

	// Get URL and title from the corresponding result
	result := results[idx]
	url, _ := result["url"].(string)
	title, _ := result["title"].(string)

	if url == "" {
		return nil
	}

	// Build encrypted_index as base64-encoded JSON
	payload := map[string]string{
		"url":        url,
		"title":      title,
		"cited_text": citedText,
	}
	payloadJSON, _ := json.Marshal(payload)
	encryptedIndex := base64.StdEncoding.EncodeToString(payloadJSON)

	return &Citation{
		Type:           "web_search_result_location",
		CitedText:      citedText,
		URL:            url,
		Title:          title,
		EncryptedIndex: encryptedIndex,
	}
}

// buildCitationTextBlocks creates text blocks with citations for non-streaming response
// Each citation becomes a separate text block with empty text and citations array
func buildCitationTextBlocks(supports gjson.Result, results []map[string]interface{}) []map[string]interface{} {
	var blocks []map[string]interface{}

	if !supports.IsArray() {
		return blocks
	}

	for _, support := range supports.Array() {
		citation := buildCitation(support, results)
		if citation == nil {
			continue
		}

		block := map[string]interface{}{
			"type": "text",
			"text": "",
			"citations": []map[string]interface{}{
				{
					"type":            citation.Type,
					"cited_text":      citation.CitedText,
					"url":             citation.URL,
					"title":           citation.Title,
					"encrypted_index": citation.EncryptedIndex,
				},
			},
		}
		blocks = append(blocks, block)
	}

	return blocks
}

// buildCitationsForSSE extracts citations for streaming response
// Returns a slice of Citation objects
func buildCitationsForSSE(supports gjson.Result, results []map[string]interface{}) []*Citation {
	var citations []*Citation

	if !supports.IsArray() {
		return citations
	}

	for _, support := range supports.Array() {
		citation := buildCitation(support, results)
		if citation != nil {
			citations = append(citations, citation)
		}
	}

	return citations
}
