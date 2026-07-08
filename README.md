# Kimi Proxy Bridge

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg)](https://go.dev/)
[![API](https://img.shields.io/badge/API-OpenAI%20Compatible-orange.svg)]()
[![Performance](https://img.shields.io/badge/Optimized-High%20Throughput-red.svg)]()

A high-performance, production-ready Go proxy server that provides an
OpenAI-compatible API interface for Kimi AI (kimi.ai). Built for maximum
network throughput with zero external dependencies, connection pooling,
HTTP/2 support, and dynamic model discovery.

## ✨ Features

- **⚡ Go-Native Performance** — Compiled binary with zero GC pressure on hot paths, pooled HTTP connections, and HTTP/2 multiplexing
- **🔌 OpenAI-Compatible API** — Drop-in replacement for OpenAI chat completions endpoint
- **🌊 SSE Streaming** — Server-Sent Events for real-time token streaming with per-flush control
- **🖼️ Multimodal Support** — Handles both text and structured content arrays
- **🧠 Extended Capabilities** — Deep thinking and web search functionality
- **📚 Flexible History** — Toggle between stateful conversations and stateless requests
- **🔄 Dynamic Model Discovery** — Models fetched live from Kimi server at startup; refreshable at runtime
- **🔒 Thread-Safe** — `sync.RWMutex` protected state for concurrent request handling
- **💤 Graceful Shutdown** — Clean connection draining on SIGINT/SIGTERM

## 📦 Prerequisites

- **Go**: Version 1.21 or higher
- **Kimi.ai Account**: Valid authentication credentials
- **Network Access**: Connectivity to `kimi.com` services

## 🚀 Installation

### Quick Start

```bash
# Initialize module
go mod init kimi-proxy

# Export token
export KIMI_ACCESS_TOKEN="ey..." # or $env:KIMI_ACCESS_TOKEN="ey..." for Windows
# Run directly (development)
go run main.go
```

### Production Build (Recommended)

```bash
# Build stripped, optimized binary
go build -ldflags "-s -w" -trimpath -o kimi-proxy main.go

# Run
KIMI_ACCESS_TOKEN="ey.." ./kimi-proxy
```

### Environment Variables (Optional)

| Variable | Default | Description |
|----------|---------|-------------|
| `KIMI_ACCESS_TOKEN` | `""` | Kimi access token (JWT) |
| `AUTH_KEY` | `Waguri` | API key clients must send as `Bearer <key>` |
| `PORT` | `3000` | Server listen port |

You can also hardcode credentials directly in `main.go` (lines 17-19).

## 🔐 Configuration

### Obtaining Your Access Token

1. Navigate to [kimi.ai](https://kimi.ai) and log in
2. Open **Developer Tools** (`F12`)
3. Go to **Application** → **Local Storage** → `https://kimi.ai`
4. Copy the `access_token` value (JWT starting with `eyJ...`)

**Via Console:**
```javascript
localStorage.getItem('access_token')
```

### Setting the Token

**Option A — Environment variable (recommended):**
```bash
export KIMI_ACCESS_TOKEN="eyJhbGciOi..."
./kimi-proxy
```

**Option B — Hardcode in source:**
```go
accessToken = envOrDefault("KIMI_ACCESS_TOKEN", "your-token-here")
```

### Client Authentication

All API requests must include:
```http
Authorization: Bearer Waguri
```

Customize via `AUTH_KEY` env var or source code.

### Quick Test

```bash
curl http://localhost:3000/models \
  -H "Authorization: Bearer Waguri"
```

## 📡 API Reference

### 1. Chat Completions

**Endpoint:** `POST /v1/chat/completions`

#### Request Body
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `messages` | Array | Yes | Conversation history |
| `model` | String | No | Model key (e.g. `k2d6`, `k2d6-thinking`). Defaults to server default |
| `stream` | Boolean | No | SSE streaming (always streamed) |
| `deepThink` | Boolean | No | Enable enhanced reasoning |
| `search` | Boolean | No | Enable web search |

#### Basic Example
```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer Waguri" \
  -H "Content-Type: application/json" \
  -N \
  -d '{
    "messages": [{"role": "user", "content": "Hello!"}],
    "model": "k2d6"
  }'
```

#### Deep Thinking Mode
```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer Waguri" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Solve: 2x + 5 = 15"}],
    "model": "k2d6-thinking",
    "deepThink": true
  }'
```

### 2. Model Management

**List Available Models (fetched dynamically from Kimi):**
```bash
GET /models
```

**Switch Default Model:**
```bash
POST /models
Content-Type: application/json

{"model": "k2d6-thinking"}
```

**Refresh Models from Server:**
```bash
POST /refresh-models
```

Models are fetched at startup from:
```
POST https://www.kimi.com/apiv2/kimi.gateway.config.v1.ConfigService/GetAvailableModels
```

Example model keys returned:

| Key | Display Name | Scenario | Thinking |
|-----|-------------|----------|----------|
| `k2d6` | K2.6 Instant | SCENARIO_K2D5 | No |
| `k2d6-thinking` | K2.6 Thinking | SCENARIO_K2D5 | Yes |
| `k2d6-agent` | K2.6 Agent | SCENARIO_OK_COMPUTER | No |
| `k2d6-agent-ultra` | K2.6 Agent Swarm | SCENARIO_OK_COMPUTER | No |

*Model keys are dynamic and may change based on Kimi server configuration.*

### 3. History Mode

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/history?enable=true` | GET | Toggle history mode |
| `/history` | POST | `{"enable": true}` |

| Mode | Behavior |
|------|----------|
| `false` (default) | Stateless — each request branches from initialization point |
| `true` | Stateful — maintains continuous conversation with dynamic message IDs |

### 4. Session Management

```bash
POST /new
```

Initializes a fresh chat session with new Chat ID and Parent Message ID.

## 🛠️ Technical Details

### Performance Architecture

- **Connection Pooling**: `http.Transport` with 500 max idle connections, 100 per-host
- **HTTP/2**: Auto-negotiated with `ForceAttemptHTTP2`
- **Buffered I/O**: `bufio.NewReaderSize(64KB)` for upstream frame parsing
- **Zero-Copy Reads**: Pre-allocated SSE framing byte slices
- **Thread-Safe State**: `sync.RWMutex` with read-lock for hot paths
- **Reusable Chunk Struct**: OpenAI SSE chunk template reused across frames

### Connect Protocol

Kimi uses the [Connect RPC](https://connect.build/) protocol for streaming:

```
Wire format: [1-byte flag] [4-byte BE length] [JSON payload]
  flag 0x00 = data frame
  flag 0x02 = error/trailer frame (skipped)
```

### State Management

- **Static IDs**: Created on startup or via `/new`. Used when `history: false`
- **Dynamic IDs**: Updated per-interaction when `history: true`
- **Model Context**: Per-request override via `model` field, or global default via `POST /models`

## 🐛 Troubleshooting

| Issue | Solution |
|-------|----------|
| `401 Unauthorized` | Verify `access_token` is valid; regenerate from kimi.ai |
| `Connection refused` | Ensure server is running on configured port |
| Empty responses | Check `Authorization: Bearer Waguri` header |
| Streaming not working | Ensure client supports SSE; use `-N` flag in curl |
| Model not found | `GET /models` to list valid keys; `POST /refresh-models` to update |
| `503 Server not ready` | Server still initializing chat session — retry in a moment |

---

**Note**: Unofficial community project. Not affiliated with Moonshot AI or kimi.ai.
