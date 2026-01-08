# cpa_websearch_proxy

A lightweight proxy that adds `web_search` support to Claude Code via Gemini's `googleSearch`. Designed for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) with Antigravity authentication.

## Overview

```
Claude Code --> cpa_websearch_proxy --> CLIProxyAPI 
                     |
                     +--> (web_search) --> Gemini googleSearch
```

- Intercepts `web_search` tool requests and routes to Gemini
- Forwards all other requests to upstream (CLIProxyAPI or other)
- Uses Gemini API key for authentication


## Quick Start

1. Copy and edit config file:
   ```bash
   cp config.example.yaml config.yaml
   # Edit config.yaml to set gemini_api_key or auth_file
   ```

2. Run:
   ```bash
   ./cpa_websearch_proxy
   ```

3. Configure Claude Code:
   ```bash
   export ANTHROPIC_BASE_URL="http://localhost:8318"
   ```

## Installation

```bash
go build .
```

Or download from [Releases](https://github.com/aprils148/cpa_websearch_proxy/releases).

## Configuration

See `config.example.yaml` for all available options.

```bash
cp config.example.yaml config.yaml
```

## License

MIT License
