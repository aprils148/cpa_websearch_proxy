package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cliproxyapi/cpa_websearch_proxy/internal"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to config file")
	port := flag.Int("port", 0, "Listen port (overrides config)")
	showHelp := flag.Bool("help", false, "Show help message")
	flag.Parse()

	if *showHelp {
		printUsage()
		os.Exit(0)
	}

	// Load configuration
	cfg, err := internal.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override port if specified on command line
	if *port != 0 {
		cfg.ListenPort = *port
	}

	// Validate Gemini API key
	if cfg.GeminiAPIKey == "" {
		log.Fatal("GEMINI_API_KEY is required. Set it via environment variable or config file.")
	}

	if cfg.UpstreamURL == "" {
		log.Println("Warning: No upstream_url configured. Non-web_search requests will fail.")
		log.Println("  Set UPSTREAM_URL env var or upstream_url in config.yaml")
	}

	// Create proxy server
	proxy := internal.NewProxy(cfg)

	// Print startup info
	host := cfg.ListenHost
	if host == "" {
		host = internal.DefaultListenHost
	}
	addr := fmt.Sprintf("%s:%d", host, cfg.ListenPort)
	log.Println("========================================")
	log.Println("  cpa_websearch_proxy for Claude Code")
	log.Println("========================================")
	log.Printf("Listen address: http://%s", addr)
	if cfg.UpstreamURL != "" {
		log.Printf("Upstream:       %s", cfg.UpstreamURL)
	} else {
		log.Println("Upstream:       (not configured)")
	}
	log.Printf("Search model:   %s", cfg.WebSearchModel)
	log.Printf("Log level:      %s", cfg.LogLevel)
	log.Println("----------------------------------------")
	log.Println("Configure Claude Code:")
	log.Printf("  export ANTHROPIC_BASE_URL=http://%s", addr)
	log.Println("========================================")

	// Start HTTP server
	srv := &http.Server{
		Addr:              addr,
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MiB
	}

	// Set up graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server failed: %v", err)
	}
}

func printUsage() {
	fmt.Print(`cpa_websearch_proxy - Add web_search to Claude via Gemini

USAGE:
  cpa_websearch_proxy [OPTIONS]

OPTIONS:
  -port <port>        Listen port (default: 8318)
  -config <path>      Path to config file (default: config.yaml)
  -help               Show this help message

ENVIRONMENT VARIABLES:
  GEMINI_API_KEY      Gemini API key (required)
  UPSTREAM_URL        Claude API proxy URL (default: http://localhost:8317)
  LISTEN_HOST         Listen host (default: 127.0.0.1)
  LISTEN_PORT         Listen port (default: 8318)
  WEB_SEARCH_MODEL    Gemini model for web search (default: gemini-2.5-flash)
  GEMINI_API_BASE_URL Gemini API base URL (defaults to UPSTREAM_URL)
  LOG_LEVEL           debug, info, warn, error (default: info)

EXAMPLE:
  export GEMINI_API_KEY="AIza..."
  export UPSTREAM_URL="http://localhost:8317"
  cpa_websearch_proxy

  # Then configure Claude Code
  export ANTHROPIC_BASE_URL="http://127.0.0.1:8318"
`)
}
