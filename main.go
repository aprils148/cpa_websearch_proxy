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
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to config file")
	port := flag.Int("port", 0, "Listen port (overrides config)")
	authFile := flag.String("auth-file", "", "Path to CLIProxyAPI auth file or directory")
	showHelp := flag.Bool("help", false, "Show help message")
	flag.Parse()

	if *showHelp {
		printUsage()
		os.Exit(0)
	}

	// Load configuration
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override port if specified on command line
	if *port != 0 {
		cfg.ListenPort = *port
	}

	// Override auth file if specified on command line
	if *authFile != "" {
		cfg.AuthFile = *authFile
	}

	// Determine auth mode
	useGeminiAPI := cfg.UseGeminiAPI()

	// Create auth manager and load auth files (only for Antigravity mode)
	var authMgr *AuthManager
	if !useGeminiAPI && cfg.AuthFile != "" {
		cooldown := time.Duration(cfg.AuthFailCooldown) * time.Second
		authMgr = NewAuthManager(cooldown)

		if err := authMgr.LoadFromDirectory(cfg.AuthFile); err != nil {
			log.Fatalf("Failed to load auth: %v", err)
		}

		authFiles := authMgr.ListAuthFiles()
		if len(authFiles) == 1 {
			log.Printf("Loaded 1 auth file: %s", filepath.Base(authFiles[0]))
		} else {
			log.Printf("Loaded %d auth files (will rotate on failure):", len(authFiles))
			for _, f := range authFiles {
				log.Printf("  - %s", filepath.Base(f))
			}
		}
	}

	// Validate configuration
	if useGeminiAPI {
		log.Println("Using Gemini API key mode")
	} else {
		hasAuth := authMgr != nil && authMgr.Count() > 0
		if !hasAuth {
			log.Println("Warning: No auth configured. Web search will not work.")
			log.Println("  Use -auth-file for Antigravity mode, or set GEMINI_API_KEY for Gemini API mode")
		}
	}

	if cfg.UpstreamURL == "" {
		log.Println("Warning: No upstream_url configured. Non-web_search requests will fail.")
		log.Println("  Set UPSTREAM_URL env var or upstream_url in config.yaml")
	}

	// Create proxy server
	proxy := NewProxy(cfg, authMgr)

	// Print startup info
	host := cfg.ListenHost
	if host == "" {
		host = DefaultListenHost
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
	if useGeminiAPI {
		log.Println("Auth mode:      Gemini API key")
		log.Printf("Search model:   %s", cfg.WebSearchModel)
	} else if authMgr != nil && authMgr.Count() > 0 {
		log.Println("Auth mode:      Antigravity")
		if authMgr.Count() > 1 {
			log.Printf("Auth files:     %d (auto-rotate on failure)", authMgr.Count())
		}
		log.Printf("Search model:   %s", cfg.WebSearchModel)
	}
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
  -auth-file <path>   Path to auth file or directory (Antigravity mode)
  -help               Show this help message

ENVIRONMENT VARIABLES:
  GEMINI_API_KEY      Gemini API key (recommended, simplest setup)
  UPSTREAM_URL        CLIProxyAPI URL (default: http://localhost:8317)
  AUTH_FILE           Path to auth file or directory (Antigravity mode)
  LISTEN_HOST         Listen host (default: 127.0.0.1)
  LISTEN_PORT         Listen port
  WEB_SEARCH_MODEL    Gemini model for web search (default: gemini-2.5-flash)
  LOG_LEVEL           debug, info, warn, error

AUTH MODES:
  1. Gemini API key (recommended):
     export GEMINI_API_KEY="your-api-key"
     cpa_websearch_proxy

  2. Antigravity (via CLIProxyAPI auth files):
     cpa_websearch_proxy -auth-file ~/.cli-proxy-api/

EXAMPLE:
  # Using Gemini API key
  export GEMINI_API_KEY="AIza..."
  export UPSTREAM_URL="http://localhost:8317"
  cpa_websearch_proxy

  # Then configure Claude Code
  export ANTHROPIC_BASE_URL="http://127.0.0.1:8318"
`)
}
