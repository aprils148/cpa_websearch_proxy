package main

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the websearch proxy
type Config struct {
	// Listen host for the proxy (default: 127.0.0.1)
	ListenHost string `yaml:"listen_host"`

	// Listen port for the proxy
	ListenPort int `yaml:"listen_port"`

	// Upstream URL (CLIProxyAPI or direct Antigravity)
	UpstreamURL string `yaml:"upstream_url"`

	// OAuth client ID for Gemini/Antigravity
	ClientID string `yaml:"client_id"`

	// OAuth client secret for Gemini/Antigravity
	ClientSecret string `yaml:"client_secret"`

	// Path to CLIProxyAPI auth file or directory containing auth files
	// If a directory is specified, all antigravity-*.json files will be loaded
	// and rotated on failure
	AuthFile string `yaml:"auth_file"`

	// Cooldown period in seconds before retrying a failed auth (default: 300)
	AuthFailCooldown int `yaml:"auth_fail_cooldown"`

	// Gemini model for web search (default: gemini-2.5-flash)
	WebSearchModel string `yaml:"web_search_model"`

	// Antigravity base URL (default: production)
	AntigravityBaseURL string `yaml:"antigravity_base_url"`

	// Logging level: debug, info, warn, error
	LogLevel string `yaml:"log_level"`
}

// Default values
const (
	DefaultClientID         = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	DefaultClientSecret     = "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"
	DefaultWebSearchModel   = "gemini-2.5-flash"
	DefaultAntigravityURL   = "https://cloudcode-pa.googleapis.com"
	DefaultUpstreamURL      = "http://localhost:8317"
	DefaultListenHost       = "127.0.0.1"
	DefaultListenPort       = 8318
	DefaultLogLevel         = "info"
	DefaultAuthFailCooldown = 300 // 5 minutes
)

// LoadConfig loads configuration from a YAML file or environment variables
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		ListenHost:         DefaultListenHost,
		ListenPort:         DefaultListenPort,
		UpstreamURL:        DefaultUpstreamURL,
		ClientID:           DefaultClientID,
		ClientSecret:       DefaultClientSecret,
		WebSearchModel:     DefaultWebSearchModel,
		AntigravityBaseURL: DefaultAntigravityURL,
		LogLevel:           DefaultLogLevel,
		AuthFailCooldown:   DefaultAuthFailCooldown,
	}

	// Try to load from file
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			// File doesn't exist, fall through to env loading
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
		}
	}

	// Override with environment variables
	loadFromEnv(cfg)

	return cfg, nil
}

// loadFromEnv overrides config with environment variables
func loadFromEnv(cfg *Config) {
	if v := os.Getenv("LISTEN_HOST"); v != "" {
		cfg.ListenHost = v
	}
	if v := os.Getenv("LISTEN_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.ListenPort = port
		}
	}
	if v := os.Getenv("UPSTREAM_URL"); v != "" {
		cfg.UpstreamURL = v
	}
	if v := os.Getenv("CLIENT_ID"); v != "" {
		cfg.ClientID = v
	}
	if v := os.Getenv("CLIENT_SECRET"); v != "" {
		cfg.ClientSecret = v
	}
	if v := os.Getenv("AUTH_FILE"); v != "" {
		cfg.AuthFile = v
	}
	if v := os.Getenv("AUTH_FAIL_COOLDOWN"); v != "" {
		if cooldown, err := strconv.Atoi(v); err == nil {
			cfg.AuthFailCooldown = cooldown
		}
	}
	if v := os.Getenv("WEB_SEARCH_MODEL"); v != "" {
		cfg.WebSearchModel = v
	}
	if v := os.Getenv("ANTIGRAVITY_BASE_URL"); v != "" {
		cfg.AntigravityBaseURL = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
}
