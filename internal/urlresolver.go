package internal

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	vertexRedirectPrefix = "https://vertexaisearch.cloud.google.com/grounding-api-redirect/"
	resolveTimeout       = 1500 * time.Millisecond
	maxParallelResolves  = 10
)

// URLResolver handles Vertex redirect URL resolution with caching
type URLResolver struct {
	cache      sync.Map // map[string]string
	httpClient *http.Client
}

// NewURLResolver creates a new URL resolver instance
func NewURLResolver() *URLResolver {
	return &URLResolver{
		httpClient: &http.Client{
			Timeout: resolveTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow redirects to capture final URL
				return nil
			},
		},
	}
}

// isVertexRedirectURL checks if URL is a Vertex grounding redirect
func isVertexRedirectURL(url string) bool {
	return strings.HasPrefix(url, vertexRedirectPrefix)
}

// ResolveURL resolves a single Vertex redirect URL to its final destination
// Returns original URL on any failure
func (r *URLResolver) ResolveURL(ctx context.Context, url string) string {
	// Not a vertex redirect, return as-is
	if !isVertexRedirectURL(url) {
		return url
	}

	// Check cache first
	if cached, ok := r.cache.Load(url); ok {
		return cached.(string)
	}

	// Perform resolution
	finalURL := r.doResolve(ctx, url)

	// Cache the result
	r.cache.Store(url, finalURL)

	return finalURL
}

// doResolve performs the actual HTTP request to resolve the URL
func (r *URLResolver) doResolve(ctx context.Context, url string) string {
	// Try HEAD request first (lighter)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return url
	}

	resp, err := r.httpClient.Do(req)
	if err == nil && resp != nil {
		resp.Body.Close()
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL := resp.Request.URL.String()
			if finalURL != "" && finalURL != url {
				return finalURL
			}
		}
	}

	// Fallback to GET if HEAD fails
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return url
	}

	resp, err = r.httpClient.Do(req)
	if err == nil && resp != nil {
		resp.Body.Close()
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL := resp.Request.URL.String()
			if finalURL != "" {
				return finalURL
			}
		}
	}

	// Return original URL on failure
	return url
}

// ResolveURLs resolves multiple URLs in parallel (up to first 10)
func (r *URLResolver) ResolveURLs(ctx context.Context, urls []string) []string {
	if len(urls) == 0 {
		return urls
	}

	result := make([]string, len(urls))
	copy(result, urls)

	// Limit parallel resolution to first N URLs
	limit := len(urls)
	if limit > maxParallelResolves {
		limit = maxParallelResolves
	}

	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result[idx] = r.ResolveURL(ctx, urls[idx])
		}(i)
	}
	wg.Wait()

	return result
}
