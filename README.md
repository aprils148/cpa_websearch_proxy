# cpa_websearch_proxy

A lightweight proxy that adds `web_search` support to Claude API requests via Gemini's `googleSearch`. Designed to work with CLIProxyAPI.

## Overview

```
Claude Code --> cpa_websearch_proxy --> CLIProxyAPI
                     |
                     +--> (web_search) --> Gemini googleSearch
```

- Intercepts `web_search` requests and routes to Gemini
- Forwards all other requests to CLIProxyAPI
- Supports streaming (SSE) and non-streaming responses
- Multi-auth with auto-rotation on failure

## Installation

```bash
cd cpa_websearch_proxy
go build .
```

## Quick Start

```bash
# Specify auth file path
./cpa_websearch_proxy -auth-file ~/.cli-proxy-api/
```

Then configure Claude Code:

```bash
export ANTHROPIC_BASE_URL="http://localhost:8318"
```

## Configuration

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `UPSTREAM_URL` | CLIProxyAPI URL (default: `http://localhost:8317`) | No |
| `AUTH_FILE` | Path to auth file or directory (required for `web_search`) | No |
| `LISTEN_HOST` | Listen host (default: `127.0.0.1`) | No |
| `LISTEN_PORT` | Listen port (default: 8318) | No |
| `LOG_LEVEL` | debug, info, warn, error | No |

### Config File

Create `config.yaml`:

```yaml
listen_host: "127.0.0.1"
listen_port: 8318
upstream_url: "http://localhost:8317"  # Your CLIProxyAPI
auth_file: "~/.cli-proxy-api/"
log_level: "info"
```


## Command Line Options

| Option | Description |
|--------|-------------|
| `-port <port>` | Listen port (default: 8318) |
| `-auth-file <path>` | Path to auth file or directory |

## Multi-Auth Rotation

When using a directory with multiple `antigravity-*.json` files:
- Automatically rotates to next auth on 401/403 errors
- Failed auths enter 5-minute cooldown before retry

## License

MIT License
