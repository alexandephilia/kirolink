# Kiro Claude Proxy

```
  ┌─────────────────────────────────────────────────────────────────────┐
  │                        kiro-claude-proxy                            │
  └─────────────────────────────────────────────────────────────────────┘

  ┌──────────────────┐          ┌──────────────────┐
  │   Claude Code    │          │    OpenClaw       │
  │  (Anthropic CLI) │          │  (any API client) │
  └────────┬─────────┘          └────────┬──────────┘
           │  Anthropic API                │  Anthropic API
           │  POST /v1/messages            │  POST /v1/messages
           └──────────────┬───────────────┘
                          │
                          ▼
           ┌──────────────────────────┐
           │   kiro-claude-proxy      │
           │      server :8080        │
           │                          │
           │  • Auth token inject     │
           │  • Request translation   │
           │  • Response conversion   │
           └──────────────┬───────────┘
                          │  AWS CodeWhisperer API
                          │  (via 127.0.0.1:9000)
                          ▼
           ┌──────────────────────────┐
           │   AWS CodeWhisperer      │
           │       Backend            │
           │                          │
           │  claude-sonnet / opus    │
           └──────────────────────────┘

  ──────────────────────────────────────────────────────────────────────
  Setup Flow:

    1. kiro-claude-proxy read     →  inspect token from ~/.aws/sso/cache/
    2. kiro-claude-proxy refresh  →  refresh access token
    3. kiro-claude-proxy export   →  eval $(...) sets ANTHROPIC_* env vars
    4. kiro-claude-proxy server   →  starts proxy on :8080
    5. claude                     →  Claude Code picks up ANTHROPIC_BASE_URL
  ──────────────────────────────────────────────────────────────────────
```

A Go CLI tool that hijacks Kiro's auth tokens and shoves them through an Anthropic-compatible API proxy — so your tools think they're talking to Anthropic, but you're actually riding AWS CodeWhisperer for free. Sneaky bastard, isn't it?

### Claude Code in Action

<img width="1920" height="1040" alt="image" src="Kiro-Claude-Code.png" />

## What This Beast Actually Does

- Plucks your auth token straight from `~/.aws/sso/cache/kiro-auth-token.json`
- Silently refreshes your `accessToken` before it expires, zero intervention needed
- Dumps the right environment variables so any Anthropic-compatible tool just works
- Spins up a local HTTP proxy that intercepts, translates, and forwards your requests like a goddamn middleman

## Build It

```bash
go build -o kiro-claude-proxy main.go
```

## CI/CD Pipeline

GitHub Actions handles the dirty work automatically:

- Cross-compiles release binaries for Windows, Linux, and macOS the moment you tag a new release
- Runs the full test suite on every push to `main` and every incoming Pull Request — no broken shit gets through

## How to Use This Thing

### 1. Inspect Your Token

Peek at the raw token data sitting in your cache — useful for debugging auth issues before they blow up in your face.

```bash
./kiro-claude-proxy read
```

### 2. Refresh the Token

Force-renew your access token using the stored refresh token. Run this when the proxy starts throwing 403s at you.

```bash
./kiro-claude-proxy refresh
```

### 3. Wire Up Your Environment

Inject the proxy's environment variables into your shell session so downstream tools automatically route through it.

```bash
# Linux/macOS
eval $(./kiro-claude-proxy export)

# Windows
./kiro-claude-proxy export
```

### 4. Fire Up the Proxy Server

Launch the local proxy and let it sit between your tools and AWS. Handles auth, translation, and streaming — all of it.

```bash
# Default port 8080
./kiro-claude-proxy server

# Custom port if something else is squatting on 8080
./kiro-claude-proxy server 9000
```

## Hitting the Proxy Directly

Once the server is up, point any Anthropic-compatible request at it:

1. Fire your request at the local proxy endpoint
2. The proxy injects auth headers and translates the payload to CodeWhisperer format
3. Response comes back fully converted — your client never knows the difference

```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-5-20250929","messages":[{"role":"user","content":"Hello"}],"max_tokens":256}'
```

## Token File Schema

The proxy expects this exact structure sitting in your SSO cache:

```json
{
  "accessToken": "your-access-token",
  "refreshToken": "your-refresh-token",
  "expiresAt": "2024-01-01T00:00:00Z"
}
```

## Environment Variables Exported

These two variables are all your tools need to blindly trust the proxy:

- `ANTHROPIC_BASE_URL`: `http://localhost:8080`
- `ANTHROPIC_API_KEY`: your current access token, refreshed on the fly

## Known Limitations — Don't Be Surprised

### Web Search is Dead Here

Claude Code's **native Web Search** tool is completely broken through this proxy. CodeWhisperer's backend refuses to play ball with the `tool_use`/`tool_result` round-trip that Claude Code's built-in tools depend on. It just won't happen.

**The Fix:** Swap to MCP (Model Context Protocol) servers. They run entirely on your local machine and bypass the proxy altogether — AWS never even sees the request.

Drop this into `~/.claude.json` under `"mcpServers"`:

```json
{
  "mcpServers": {
    "fetch": {
      "command": "uvx",
      "args": ["mcp-server-fetch"],
      "env": {},
      "disabled": false,
      "autoApprove": []
    },
    "exa": {
      "command": "npx",
      "args": ["-y", "exa-mcp-server"],
      "env": {
        "EXA_API_KEY": "your-exa-api-key"
      },
      "disabled": false,
      "autoApprove": []
    }
  }
}
```

- **[mcp-server-fetch](https://github.com/modelcontextprotocol/servers/tree/main/src/fetch)** — Pulls and extracts content from any URL on demand
- **[exa-mcp-server](https://github.com/exa-labs/exa-mcp-server)** — AI-native web search via [Exa](https://exa.ai) (needs an API key, worth it)

Restart Claude Code after editing the config or the new servers won't load.

## Cross-Platform — Runs Everywhere

- Windows: outputs `set` / PowerShell `$env:` syntax
- Linux/macOS: outputs `export` syntax
- Automatically resolves the correct home directory regardless of platform

## Credits

Crafted by Alexandephilia
