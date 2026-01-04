# cpa_websearch_proxy

A lightweight proxy that adds `web_search` support to Claude Code via Gemini's `googleSearch`. Designed for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) with Antigravity authentication.

## Overview

```
Claude Code --> cpa_websearch_proxy --> CLIProxyAPI (Antigravity)
                     |
                     +--> (web_search) --> Gemini googleSearch
```

- Intercepts `web_search` tool requests and routes to Gemini
- Forwards all other requests to CLIProxyAPI
- Uses Antigravity auth files from CLIProxyAPI (`~/.cli-proxy-api/antigravity-*.json`)
- Multi-auth with auto-rotation on failure

## Prerequisites

- [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) running with Antigravity login
- Auth file from `cliproxyapi -antigravity-login`

## Installation

```bash
go build .
```

Or download from [Releases](https://github.com/aprils148/cpa_websearch_proxy/releases).

## Quick Start

```bash
./cpa_websearch_proxy
```

Then configure Claude Code:

```bash
export ANTHROPIC_BASE_URL="http://localhost:8318"
```

## Configuration

See `config.example.yaml` for all available options.

```bash
cp config.example.yaml config.yaml
```

## License

MIT License
