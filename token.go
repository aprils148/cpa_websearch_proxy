package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenManager handles OAuth token refresh and caching
type TokenManager struct {
	mu           sync.RWMutex
	authManager  *AuthManager
	accessToken  string
	expiry       time.Time
	clientID     string
	clientSecret string
	httpClient   *http.Client
	// Gemini API key mode
	geminiAPIKey string
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

const (
	tokenEndpoint = "https://oauth2.googleapis.com/token"
	userAgent     = "antigravity/1.104.0 darwin/arm64"
)

// NewTokenManager creates a new token manager with AuthManager support
func NewTokenManager(cfg *Config, authMgr *AuthManager) *TokenManager {
	return &TokenManager{
		authManager:  authMgr,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		geminiAPIKey: cfg.GeminiAPIKey,
	}
}

// UseGeminiAPI returns true if using Gemini API key mode
func (tm *TokenManager) UseGeminiAPI() bool {
	return tm.geminiAPIKey != ""
}

// GetGeminiAPIKey returns the Gemini API key
func (tm *TokenManager) GetGeminiAPIKey() string {
	return tm.geminiAPIKey
}

// GetAccessToken returns a valid access token, refreshing if necessary
func (tm *TokenManager) GetAccessToken(ctx context.Context) (string, error) {
	tm.mu.RLock()
	// Check if we have a valid token with 5 minute buffer
	if tm.accessToken != "" && time.Now().Add(5*time.Minute).Before(tm.expiry) {
		token := tm.accessToken
		tm.mu.RUnlock()
		return token, nil
	}
	tm.mu.RUnlock()

	return tm.refresh(ctx)
}

// getRefreshToken returns the current refresh token (from AuthManager or single token)
func (tm *TokenManager) getRefreshToken() (string, error) {
	if tm.authManager != nil && tm.authManager.Count() > 0 {
		return tm.authManager.GetCurrentRefreshToken()
	}
	return "", fmt.Errorf("no refresh token configured")
}

// refresh obtains a new access token using the refresh token
func (tm *TokenManager) refresh(ctx context.Context) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Double-check after acquiring write lock
	if tm.accessToken != "" && time.Now().Add(5*time.Minute).Before(tm.expiry) {
		return tm.accessToken, nil
	}

	refreshToken, err := tm.getRefreshToken()
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("client_id", tm.clientID)
	form.Set("client_secret", tm.clientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.Error != "" {
		return "", fmt.Errorf("token refresh failed: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	tm.accessToken = tokenResp.AccessToken
	tm.expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return tm.accessToken, nil
}

// InvalidateToken clears the cached access token, forcing a refresh on next call
func (tm *TokenManager) InvalidateToken() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.accessToken = ""
	tm.expiry = time.Time{}
}

// MarkAuthFailed marks the current auth as failed and switches to next one
// Returns true if a new auth is available, false if all auths failed
func (tm *TokenManager) MarkAuthFailed() bool {
	tm.InvalidateToken()
	if tm.authManager != nil {
		return tm.authManager.MarkCurrentFailed()
	}
	return false
}

// MarkAuthSuccess marks the current auth as successful
func (tm *TokenManager) MarkAuthSuccess() {
	if tm.authManager != nil {
		tm.authManager.ResetCurrentFailCount()
	}
}
