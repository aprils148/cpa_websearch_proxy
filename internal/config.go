package internal

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

	// Upstream URL (CLIProxyAPI or other Claude API proxy)
	UpstreamURL string `yaml:"upstream_url"`

	// Gemini API key for web search
	GeminiAPIKey string `yaml:"gemini_api_key"`

	// Gemini model for web search (default: gemini-2.5-flash)
	WebSearchModel string `yaml:"web_search_model"`

	// Gemini API base URL (defaults to UpstreamURL if not set)
	GeminiAPIBaseURL string `yaml:"gemini_api_base_url"`

	// Logging level: debug, info, warn, error
	LogLevel string `yaml:"log_level"`
}

// Default values
const (
	DefaultWebSearchModel = "gemini-2.5-flash"
	DefaultUpstreamURL    = "http://localhost:8317"
	DefaultListenHost     = "127.0.0.1"
	DefaultListenPort     = 8318
	DefaultLogLevel       = "info"
)

// LoadConfig loads configuration from a YAML file or environment variables
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		ListenHost:     DefaultListenHost,
		ListenPort:     DefaultListenPort,
		UpstreamURL:    DefaultUpstreamURL,
		WebSearchModel: DefaultWebSearchModel,
		LogLevel:       DefaultLogLevel,
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

	// Set GeminiAPIBaseURL to UpstreamURL if not explicitly configured
	if cfg.GeminiAPIBaseURL == "" {
		cfg.GeminiAPIBaseURL = cfg.UpstreamURL
	}

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
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.GeminiAPIKey = v
	}
	if v := os.Getenv("WEB_SEARCH_MODEL"); v != "" {
		cfg.WebSearchModel = v
	}
	if v := os.Getenv("GEMINI_API_BASE_URL"); v != "" {
		cfg.GeminiAPIBaseURL = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
}
