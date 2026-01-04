package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const maxRequestBodyBytes int64 = 64 << 20 // 64MiB, virtually unreachable in normal use

// Proxy handles HTTP requests, intercepting web_search requests
type Proxy struct {
	cfg           *Config
	upstreamProxy *httputil.ReverseProxy
	tokenManager  *TokenManager
	geminiClient  *GeminiClient
	authManager   *AuthManager
	urlResolver   *URLResolver
	debug         bool
}

// NewProxy creates a new proxy instance
func NewProxy(cfg *Config, authMgr *AuthManager) *Proxy {
	tm := NewTokenManager(cfg, authMgr)
	gc := NewGeminiClient(cfg, tm, authMgr)

	p := &Proxy{
		cfg:          cfg,
		tokenManager: tm,
		geminiClient: gc,
		authManager:  authMgr,
		urlResolver:  NewURLResolver(),
		debug:        cfg.LogLevel == "debug",
	}

	// Set up reverse proxy if upstream URL is configured
	if cfg.UpstreamURL != "" {
		upstream, err := url.Parse(cfg.UpstreamURL)
		if err != nil {
			log.Fatalf("Invalid upstream URL: %v", err)
		}

		reverseProxy := httputil.NewSingleHostReverseProxy(upstream)
		originalDirector := reverseProxy.Director
		reverseProxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = upstream.Host
		}
		p.upstreamProxy = reverseProxy
	}

	return p
}

// ServeHTTP implements http.Handler
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only intercept POST requests to messages endpoint
	path := strings.TrimRight(r.URL.Path, "/")
	if r.Method != http.MethodPost || !strings.HasSuffix(path, "/messages") {
		p.proxyOrReject(w, r)
		return
	}

	// Read request body
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if ok := errors.As(err, &maxBytesErr); ok {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// Check if this is a Claude model with web_search tool
	model := GetModel(body)
	if !IsClaudeModel(model) || !HasWebSearchTool(body) {
		// Not a web_search request, proxy through
		if p.debug {
			log.Printf("Proxying request (no web_search): %s", r.URL.Path)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		p.proxyOrReject(w, r)
		return
	}

	// Handle web_search request
	if p.authManager != nil && p.authManager.Count() > 1 {
		log.Printf("web_search detected for model %s, routing to Gemini (using %s)",
			model, p.authManager.GetCurrentAuthPath())
	} else {
		log.Printf("web_search detected for model %s, routing to Gemini", model)
	}
	p.handleWebSearch(w, r, body, model)
}

// proxyOrReject either proxies the request or returns an error if no upstream
func (p *Proxy) proxyOrReject(w http.ResponseWriter, r *http.Request) {
	if p.upstreamProxy != nil {
		p.upstreamProxy.ServeHTTP(w, r)
	} else {
		http.Error(w, "No upstream configured and request is not a web_search request", http.StatusBadGateway)
	}
}

// handleWebSearch processes a web_search request via Gemini
// Now passes full conversation history to Gemini for better context
func (p *Proxy) handleWebSearch(w http.ResponseWriter, r *http.Request, body []byte, model string) {
	ctx := r.Context()

	if p.debug {
		query := ExtractUserQuery(body)
		sum := sha256.Sum256([]byte(query))
		log.Printf("Executing web search with full conversation history (last_query_bytes=%d, last_query_sha256=%s)",
			len(query), hex.EncodeToString(sum[:]))
	}

	// Execute Gemini web search with full Claude payload (conversation history)
	geminiResp, err := p.geminiClient.ExecuteWebSearch(ctx, body)
	if err != nil {
		log.Printf("Gemini web search failed: %v", err)
		http.Error(w, "Web search temporarily unavailable", http.StatusBadGateway)
		return
	}

	if p.debug {
		log.Printf("Gemini response received, converting to Claude format with URL resolution and citations")
	}

	// Check if streaming
	if IsStreamingRequest(body) {
		p.writeSSEResponse(ctx, w, model, geminiResp)
	} else {
		p.writeNonStreamResponse(ctx, w, model, geminiResp)
	}
}

// writeNonStreamResponse writes a non-streaming Claude response
func (p *Proxy) writeNonStreamResponse(ctx context.Context, w http.ResponseWriter, model string, geminiResp []byte) {
	response := ConvertToClaudeNonStream(ctx, model, geminiResp, p.urlResolver)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

// writeSSEResponse writes a streaming SSE Claude response
func (p *Proxy) writeSSEResponse(ctx context.Context, w http.ResponseWriter, model string, geminiResp []byte) {
	events := ConvertToClaudeSSEStream(ctx, model, geminiResp, p.urlResolver)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fallback: write all at once
		for _, event := range events {
			w.Write([]byte(event))
		}
		return
	}

	for _, event := range events {
		w.Write([]byte(event))
		flusher.Flush()
	}
}
