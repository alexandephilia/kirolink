# Kiro Claude Proxy

[![Claude Code](https://img.shields.io/badge/Claude_Code-integrated-7B2D8B?logo=anthropic&logoColor=white)](https://claude.ai/code)
![Go](https://img.shields.io/badge/Go-1.23.3-00ADD8?logo=go&logoColor=white)
![API](https://img.shields.io/badge/API-Anthropic_compatible-111111?logo=anthropic&logoColor=white)

`kiro-claude-proxy` is a small Go CLI that reads Kiro auth tokens from your local SSO cache, then exposes an Anthropic-shaped local API so tools like Claude Code can talk to it without a bunch of manual bullshit.

In practice: your client sends `POST /v1/messages` to this proxy, the proxy translates the request to AWS CodeWhisperer, forwards it through the local backend hop, and translates the response back on the way out.

![Claude Code using the proxy](Claude-Code.jpg)

## What this thing actually does

- Reads tokens from `~/.aws/sso/cache/kiro-auth-token.json`
- Prints shell-ready `ANTHROPIC_*` environment variable setup
- Starts a local server on port `8080` by default
- Exposes these endpoints:
  - `POST /v1/messages`
  - `GET /v1/models`
  - `GET /health`
- Includes a `claude` helper command that edits `~/.claude.json`

## Quick start

### 1. Build it

```bash
go build -o kiro-claude-proxy main.go
```

### 2. Make sure Kiro is already logged in

This tool expects a token file at:

```text
~/.aws/sso/cache/kiro-auth-token.json
```

If you want to sanity-check that file first:

```bash
./kiro-claude-proxy read
```

Heads-up: `read` prints both the access token and refresh token, so maybe don't paste that shit into screenshots.

### 3. Export the Anthropic env vars

On macOS/Linux, you can eval the output directly:

```bash
eval "$(./kiro-claude-proxy export)"
```

On Windows, the command prints both CMD and PowerShell variants for you to copy:

```bash
./kiro-claude-proxy export
```

By default this sets:

- `ANTHROPIC_BASE_URL=http://localhost:8080`
- `ANTHROPIC_API_KEY=<current access token>`

### 4. Start the proxy

```bash
./kiro-claude-proxy server
```

Custom port:

```bash
./kiro-claude-proxy server 9000
```

If you use a custom port, set `ANTHROPIC_BASE_URL` manually — the `export` command always prints `http://localhost:8080`.

### 5. Point your client at it

Claude Code and other Anthropic-compatible clients can use the exported env vars. You can also hit the proxy directly:

```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-5-20250929","messages":[{"role":"user","content":"Hello"}],"max_tokens":256}'
```

## Commands

| Command                             | What it does                                                                                 |
| ----------------------------------- | -------------------------------------------------------------------------------------------- |
| `./kiro-claude-proxy read`          | Reads and prints the cached token data.                                                      |
| `./kiro-claude-proxy refresh`       | Refreshes the token using the stored refresh token and writes the updated file back to disk. |
| `./kiro-claude-proxy export`        | Prints environment variable commands for the current OS/shell style.                         |
| `./kiro-claude-proxy claude`        | Updates `~/.claude.json` and sets `hasCompletedOnboarding=true` plus `kiro2cc=true`.         |
| `./kiro-claude-proxy server [port]` | Starts the local Anthropic-compatible proxy server.                                          |

## HTTP surface

When the server is running, these routes are available:

- `POST /v1/messages` — main Anthropic-compatible message endpoint
- `GET /v1/models` — returns the currently exposed model aliases
- `GET /health` — returns `OK`

Example:

```bash
curl http://localhost:8080/v1/models
```

## Model aliases

The proxy currently exposes multiple Anthropic-style aliases, including:

- `default`
- `claude-sonnet-4-6`
- `claude-sonnet-4-5`
- `claude-opus-4-6`
- `claude-haiku-4-5-20251001`
- `claude-4-sonnet`
- `claude-4-opus`
- `claude-5-sonnet`

If you want the full live list, ask the running server with `GET /v1/models`.

## How it works

1. Read the token from your local Kiro SSO cache.
2. Accept Anthropic-style requests over HTTP.
3. Translate them into the backend request format.
4. Forward them through the local upstream hop at `127.0.0.1:9000`.
5. Translate the response back into Anthropic-style JSON or SSE.

## Development

Build:

```bash
go build -o kiro-claude-proxy main.go
```

Run tests:

```bash
go test ./...
```

Run parser tests only:

```bash
go test ./parser -v
```

## Rough edges you should know about

- This tool depends on a local Kiro token file already existing.
- `refresh` writes back to `~/.aws/sso/cache/kiro-auth-token.json`.
- `claude` modifies `~/.claude.json`; that's convenient, but it's still changing your config, so don't run it blindly.
- The documented export path is hardcoded to `http://localhost:8080`.
- The upstream backend hop is hardcoded to `127.0.0.1:9000`.

## Credit

Crafted by Alexandephilia.
