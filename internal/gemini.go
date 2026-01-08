package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// GeminiClient handles web search requests via Gemini's googleSearch
type GeminiClient struct {
	apiBaseURL string
	apiKey     string
	model      string
	httpClient *http.Client
	debug      bool
}

const (
	geminiAPIGeneratePath = "/v1beta/models/%s:generateContent"
	userAgent             = "cpa-websearch-proxy/1.0"
)

// NewGeminiClient creates a new Gemini client for web search
func NewGeminiClient(cfg *Config) *GeminiClient {
	return &GeminiClient{
		apiBaseURL: strings.TrimSuffix(cfg.GeminiAPIBaseURL, "/"),
		apiKey:     cfg.GeminiAPIKey,
		model:      cfg.WebSearchModel,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		debug:      cfg.LogLevel == "debug",
	}
}

// ExecuteWebSearch performs a web search using Gemini's googleSearch tool
func (gc *GeminiClient) ExecuteWebSearch(ctx context.Context, claudePayload []byte) ([]byte, error) {
	if len(claudePayload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}

	return gc.executeRequest(ctx, claudePayload)
}

// executeRequest performs the web search request
func (gc *GeminiClient) executeRequest(ctx context.Context, claudePayload []byte) ([]byte, error) {
	reqURL := gc.apiBaseURL + fmt.Sprintf(geminiAPIGeneratePath, gc.model) + "?key=" + gc.apiKey

	// Build request payload
	payload, err := gc.buildRequest(claudePayload)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Debug: log request details
	if gc.debug {
		log.Printf("[DEBUG] Gemini Request URL: %s", gc.sanitizeURL(reqURL))
		log.Printf("[DEBUG] Gemini Request Summary: %s", summarizeGeminiRequest(payload))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader([]byte(payload)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	if gc.debug {
		log.Printf("[DEBUG] Request Headers: Content-Type=%s, User-Agent=%s (API key in URL)",
			"application/json", userAgent)
	}

	resp, err := gc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read gemini response: %w", err)
	}

	// Debug: log response
	if gc.debug {
		log.Printf("[DEBUG] Gemini Response Status: %d", resp.StatusCode)
		log.Printf("[DEBUG] Gemini Response Summary: %s", summarizeGeminiResponse(body))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini returned status %d (response_bytes=%d, response_sha256=%s)",
			resp.StatusCode, len(body), sha256Hex(body))
	}

	return body, nil
}

// sanitizeURL removes API key from URL for logging
func (gc *GeminiClient) sanitizeURL(url string) string {
	if idx := strings.Index(url, "?key="); idx != -1 {
		return url[:idx] + "?key=<redacted>"
	}
	return url
}

// buildRequest constructs the request payload for Gemini web search
func (gc *GeminiClient) buildRequest(claudePayload []byte) (string, error) {
	// Transform Claude messages to Gemini contents format
	contents, err := TransformMessages(claudePayload)
	if err != nil {
		return "", fmt.Errorf("failed to transform messages: %w", err)
	}

	// Fallback: if no messages transformed, extract last user query (backward compatibility)
	if len(contents) == 0 {
		query := ExtractUserQuery(claudePayload)
		if query == "" {
			return "", fmt.Errorf("no messages found in payload")
		}
		contents = []GeminiContent{
			{
				Role: "user",
				Parts: []GeminiPart{
					{Text: query},
				},
			},
		}
	}

	// Convert contents to JSON
	contentsJSON, err := json.Marshal(contents)
	if err != nil {
		return "", fmt.Errorf("failed to marshal contents: %w", err)
	}

	// Gemini API format: {"contents":[], "tools":[{"googleSearch":{}}]}
	req := `{"contents":[],"tools":[{"googleSearch":{}}]}`

	// Set contents
	req, _ = sjson.SetRaw(req, "contents", string(contentsJSON))

	return req, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func summarizeGeminiRequest(payload string) string {
	b := []byte(payload)

	summary := map[string]interface{}{
		"bytes":  len(b),
		"sha256": sha256Hex(b),
	}

	if contents := gjson.GetBytes(b, "contents"); contents.IsArray() {
		summary["contents_count"] = len(contents.Array())
	}
	if tools := gjson.GetBytes(b, "tools"); tools.IsArray() {
		summary["tools_count"] = len(tools.Array())
	}

	out, err := json.Marshal(summary)
	if err != nil {
		return fmt.Sprintf("bytes=%d sha256=%s", len(b), sha256Hex(b))
	}
	return string(out)
}

func summarizeGeminiResponse(resp []byte) string {
	summary := map[string]interface{}{
		"bytes":  len(resp),
		"sha256": sha256Hex(resp),
	}

	// candidates count
	candidates := gjson.GetBytes(resp, "candidates")
	if candidates.IsArray() {
		summary["candidates_count"] = len(candidates.Array())
	}

	groundingMetadata := extractGroundingMetadata(resp)
	if groundingMetadata.Exists() {
		if chunks := groundingMetadata.Get("groundingChunks"); chunks.IsArray() {
			summary["grounding_chunks"] = len(chunks.Array())
		}
		if queries := groundingMetadata.Get("webSearchQueries"); queries.IsArray() {
			summary["web_search_queries"] = len(queries.Array())
		}
	}

	if supports := extractGroundingSupports(resp); supports.IsArray() {
		summary["grounding_supports"] = len(supports.Array())
	}

	out, err := json.Marshal(summary)
	if err != nil {
		return fmt.Sprintf("bytes=%d sha256=%s", len(resp), sha256Hex(resp))
	}
	return string(out)
}
