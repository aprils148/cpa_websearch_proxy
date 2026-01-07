package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// GeminiClient handles web search requests via Gemini's googleSearch
type GeminiClient struct {
	// Antigravity mode
	antigravityBaseURL string
	// Gemini API mode
	geminiAPIBaseURL string
	model            string
	tokenManager     *TokenManager
	authManager      *AuthManager
	httpClient       *http.Client
	maxRetries       int
	debug            bool
}

const (
	antigravityGeneratePath = "/v1internal:generateContent"
	geminiAPIGeneratePath   = "/v1beta/models/%s:generateContent"
)

// NewGeminiClient creates a new Gemini client for web search
func NewGeminiClient(cfg *Config, tm *TokenManager, am *AuthManager) *GeminiClient {
	return &GeminiClient{
		antigravityBaseURL: strings.TrimSuffix(cfg.AntigravityBaseURL, "/"),
		geminiAPIBaseURL:   strings.TrimSuffix(cfg.GeminiAPIBaseURL, "/"),
		model:              cfg.WebSearchModel,
		tokenManager:       tm,
		authManager:        am,
		httpClient:         &http.Client{Timeout: 120 * time.Second},
		maxRetries:         5, // Maximum number of auth retries
		debug:              cfg.LogLevel == "debug",
	}
}

// UseGeminiAPI returns true if using Gemini API key mode
func (gc *GeminiClient) UseGeminiAPI() bool {
	return gc.tokenManager != nil && gc.tokenManager.UseGeminiAPI()
}

// ExecuteWebSearch performs a web search using Gemini's googleSearch tool
// It automatically retries with different auth tokens on failure
// Now accepts full Claude payload to preserve conversation history
func (gc *GeminiClient) ExecuteWebSearch(ctx context.Context, claudePayload []byte) ([]byte, error) {
	if len(claudePayload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}

	var lastErr error
	for attempt := 0; attempt <= gc.maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retrying web search (attempt %d/%d)", attempt+1, gc.maxRetries+1)
		}

		result, err := gc.executeRequest(ctx, claudePayload)
		if err == nil {
			// Success - mark auth as working
			gc.tokenManager.MarkAuthSuccess()
			return result, nil
		}

		lastErr = err

		// Check if error is auth-related (401, 403, or token errors)
		if isAuthError(err) {
			log.Printf("Auth error detected: %v", err)
			// Try to switch to next auth
			if !gc.tokenManager.MarkAuthFailed() {
				return nil, fmt.Errorf("all auth tokens failed, last error: %w", err)
			}
			continue
		}

		// Non-auth error, don't retry
		return nil, err
	}

	return nil, fmt.Errorf("max retries exceeded, last error: %w", lastErr)
}

// executeRequest performs a single web search request
func (gc *GeminiClient) executeRequest(ctx context.Context, claudePayload []byte) ([]byte, error) {
	var reqURL string
	var authHeader string

	if gc.UseGeminiAPI() {
		// Gemini API mode - use API key
		apiKey := gc.tokenManager.GetGeminiAPIKey()
		reqURL = gc.geminiAPIBaseURL + fmt.Sprintf(geminiAPIGeneratePath, gc.model) + "?key=" + apiKey
		// No Authorization header needed for API key mode
	} else {
		// Antigravity mode - use OAuth token
		token, err := gc.tokenManager.GetAccessToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get access token: %w", err)
		}
		reqURL = gc.antigravityBaseURL + antigravityGeneratePath
		authHeader = "Bearer " + token
	}

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

	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	if gc.debug {
		if authHeader != "" {
			log.Printf("[DEBUG] Request Headers: Content-Type=%s, User-Agent=%s, Authorization=Bearer <redacted>",
				"application/json", userAgent)
		} else {
			log.Printf("[DEBUG] Request Headers: Content-Type=%s, User-Agent=%s (API key in URL)",
				"application/json", userAgent)
		}
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

	// Check for auth errors
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, &AuthError{
			StatusCode: resp.StatusCode,
			BodySHA256: sha256Hex(body),
			BodyBytes:  len(body),
		}
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

// AuthError represents an authentication error
type AuthError struct {
	StatusCode int
	BodySHA256 string
	BodyBytes  int
}

func (e *AuthError) Error() string {
	if e.BodySHA256 != "" && e.BodyBytes > 0 {
		return fmt.Sprintf("auth error (status %d, response_bytes=%d, response_sha256=%s)", e.StatusCode, e.BodyBytes, e.BodySHA256)
	}
	return fmt.Sprintf("auth error (status %d)", e.StatusCode)
}

// isAuthError checks if an error is authentication-related
func isAuthError(err error) bool {
	if err == nil {
		return false
	}

	// Check for AuthError type
	if _, ok := err.(*AuthError); ok {
		return true
	}

	// Check for common auth-related error messages
	errStr := err.Error()
	authKeywords := []string{
		"401", "403",
		"unauthorized", "Unauthorized",
		"forbidden", "Forbidden",
		"invalid_grant", "invalid_token",
		"token expired", "Token expired",
		"authentication", "Authentication",
	}

	for _, keyword := range authKeywords {
		if strings.Contains(errStr, keyword) {
			return true
		}
	}

	return false
}

// buildRequest constructs the request payload for Gemini web search
// Supports both Antigravity and Gemini API formats
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

	if gc.UseGeminiAPI() {
		// Gemini API format - direct API structure
		return gc.buildGeminiAPIRequest(contentsJSON)
	}

	// Antigravity format - wrapped structure
	return gc.buildAntigravityRequest(contentsJSON)
}

// buildGeminiAPIRequest builds request for direct Gemini API
func (gc *GeminiClient) buildGeminiAPIRequest(contentsJSON []byte) (string, error) {
	// Gemini API format: {"contents":[], "tools":[{"googleSearch":{}}]}
	req := `{"contents":[],"tools":[{"googleSearch":{}}]}`

	// Set contents
	req, _ = sjson.SetRaw(req, "contents", string(contentsJSON))

	return req, nil
}

// buildAntigravityRequest builds request for Antigravity API
func (gc *GeminiClient) buildAntigravityRequest(contentsJSON []byte) (string, error) {
	// Antigravity format: {"model":"", "request":{"contents":[], "tools":[...]}, ...}
	req := `{"model":"","request":{"contents":[],"tools":[{"googleSearch":{}}]}}`

	// Set model
	req, _ = sjson.Set(req, "model", gc.model)

	// Set contents from transformed messages
	req, _ = sjson.SetRaw(req, "request.contents", string(contentsJSON))

	// Add Antigravity-specific fields
	req, _ = sjson.Set(req, "userAgent", "antigravity")

	// Use real project ID from auth if available, otherwise generate random (like CLIProxyAPI)
	projectID := ""
	if gc.authManager != nil {
		projectID = gc.authManager.GetCurrentProjectID()
	}
	if projectID != "" {
		req, _ = sjson.Set(req, "project", projectID)
	} else {
		req, _ = sjson.Set(req, "project", generateProjectID())
	}

	req, _ = sjson.Set(req, "requestId", "agent-"+uuid.NewString())

	return req, nil
}

// generateProjectID creates a project ID in the format "adjective-noun-random"
func generateProjectID() string {
	adjectives := []string{"useful", "bright", "swift", "calm", "bold", "keen", "fair", "warm"}
	nouns := []string{"fuze", "wave", "spark", "flow", "core", "beam", "link", "node"}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	adj := adjectives[r.Intn(len(adjectives))]
	noun := nouns[r.Intn(len(nouns))]
	randomPart := uuid.NewString()[:5]

	return adj + "-" + noun + "-" + randomPart
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
		"model":  gjson.GetBytes(b, "model").String(),
	}

	if v := gjson.GetBytes(b, "requestId").String(); v != "" {
		summary["requestId"] = v
	}
	if v := gjson.GetBytes(b, "project").String(); v != "" {
		summary["project"] = v
	}

	if contents := gjson.GetBytes(b, "request.contents"); contents.IsArray() {
		summary["contents_count"] = len(contents.Array())
	}
	if tools := gjson.GetBytes(b, "request.tools"); tools.IsArray() {
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

	// candidates count (support both wrapped and non-wrapped response shapes)
	candidates := gjson.GetBytes(resp, "response.candidates")
	if !candidates.IsArray() {
		candidates = gjson.GetBytes(resp, "candidates")
	}
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
