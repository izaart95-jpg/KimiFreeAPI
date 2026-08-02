# Kimi Free API

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg)](https://go.dev/)
[![API](https://img.shields.io/badge/API-OpenAI%20Compatible-orange.svg)]()
[![Performance](https://img.shields.io/badge/Optimized-High%20Throughput-red.svg)]()
[![Agent Mode](https://img.shields.io/badge/Agent%20Mode-Tool%20Calling-purple.svg)]()

A high-performance, production-ready Go proxy server that provides an
OpenAI-compatible API interface for Kimi AI (kimi.com). Built for maximum
network throughput with zero external dependencies, connection pooling,
HTTP/2 support, dynamic model discovery, and an optional **Agent Mode**
that bridges OpenAI-style tool calling onto Kimi's single-prompt upstream.

## ✨ Features

- **⚡ Go-Native Performance** — Compiled binary, pooled HTTP connections (500 idle / 100 per-host), HTTP/2 multiplexing, 64 KB buffered frame parser, pre-allocated SSE byte slices
- **🔌 OpenAI-Compatible API** — Drop-in replacement for `/v1/chat/completions`
- **🌊 SSE Streaming** — Server-Sent Events for real-time token streaming with per-flush control; non-stream JSON mode also supported in Agent Mode
- **🤖 Agent Mode (Optional)** — Translates OpenAI `messages[]` + `tools[]` into a Kimi-compatible single prompt using `[ROLE: …]` tags, and intercepts `<<<TOOL_CALL>>>` blocks in the model's output, re-emitting them as OpenAI-style `tool_calls` deltas (streaming) or as a single `chat.completion` (non-streaming)
- **🛠️ Tool Calling** — Supports user-provided tools (rendered as a text contract) and built-in Kimi tools (`web_search`, `ipython`, `show_widget`, etc.) with explicit precedence rules
- **🖼️ Multimodal Support** — Handles plain-string content and structured content arrays (`[{type:"text", text:"…"}]`)
- **🧠 Extended Capabilities** — Per-request `deepThink` (reasoning) and `search` (web search) flags
- **📚 Flexible History** — Toggle between stateful conversations (default) and stateless requests that branch from the initialization point
- **🔄 Dynamic Model Discovery** — Models fetched live from Kimi at startup; refreshable at runtime via `/refresh-models`
- **🔒 Thread-Safe** — `sync.RWMutex` protected state for concurrent request handling
- **💤 Graceful Shutdown** — Clean connection draining on `SIGINT`/`SIGTERM` (10 s timeout)
- **🌐 CORS** — Permissive CORS (`*`) for browser clients

## 📦 Prerequisites

- **Go**: Version 1.21 or higher
- **Kimi.com Account**: Valid `access_token` (JWT)
- **Network Access**: Connectivity to `www.kimi.com`

## 🚀 Installation

### Quick Start

```bash
# Initialize module
go mod init kimi-proxy

# Export token
export KIMI_ACCESS_TOKEN="ey..."           # or $env:KIMI_ACCESS_TOKEN="ey..." on Windows

# Run directly (development)
go run main.go

# Run with Agent Mode enabled
go run main.go --agent-mode
```

### Production Build (Recommended)

```bash
# Build stripped, optimized binary
go build -ldflags "-s -w" -trimpath -o kimi-proxy main.go

# Run
KIMI_ACCESS_TOKEN="ey.." ./kimi-proxy

# Run with Agent Mode
KIMI_ACCESS_TOKEN="ey.." ./kimi-proxy --agent-mode
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--agent-mode` | `false` | Enable Agent Mode: translates OpenAI tools & roles into a Kimi-compatible prompt and intercepts `<<<TOOL_CALL>>>` blocks in the output, re-emitting them as OpenAI-style `tool_calls` |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `KIMI_ACCESS_TOKEN` | `""` | Kimi access token (JWT). **Required.** |
| `AUTH_KEY` | `Waguri` | API key clients must send as `Bearer <key>` |
| `PORT` | `3000` | Server listen port |

You may also hardcode credentials directly in `main.go` (top of file).

## 🔐 Configuration

### Obtaining Your Access Token

1. Navigate to [kimi.com](https://www.kimi.com) and log in
2. Open **Developer Tools** (`F12`)
3. Go to **Application** → **Local Storage** → `https://kimi.com`
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
| `messages` | Array | Yes | Conversation history (OpenAI format) |
| `model` | String | No | Model key (e.g. `k2d6`, `k2d6-thinking`). Defaults to server default |
| `stream` | Boolean | No | See *Streaming Behavior* below |
| `deepThink` | Boolean | No | Enable enhanced reasoning (forced `true` if model has `thinking:true`) |
| `search` | Boolean | No | Enable Kimi's built-in web search tool. **Ignored in Agent Mode** (use `tools` instead) |
| `tools` | Array | No | OpenAI-style tools array. **Only used in Agent Mode** |
| `tool_choice` | Any | No | OpenAI-style tool choice hint. Currently parsed but not enforced |

#### Streaming Behavior

| Mode | `stream` field | Behavior |
|------|----------------|----------|
| Non-Agent (default) | any value (incl. absent) | **Always streamed** as SSE (preserves legacy behavior) |
| Agent Mode (`--agent-mode`) | `true` or absent | Streamed as SSE with `tool_calls` deltas |
| Agent Mode (`--agent-mode`) | `false` | Returns a single `chat.completion` JSON object |

#### Basic Example (Streamed)

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

#### Multimodal Content

```json
{
  "messages": [{
    "role": "user",
    "content": [
      {"type": "text", "text": "Describe this image"}
    ]
  }]
}
```

> Only `text` parts are extracted and forwarded to Kimi. Non-text parts are dropped.

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

The default model is auto-selected as the **first non-thinking model matching the server's default scenario**.

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
| `/history?enable=true` | GET | Toggle history mode; also returns current `chatId`/`lastMessageId` |
| `/history` | POST | `{"enable": true}` or `{"value": true}` |

| Mode | Behavior | Default |
|------|----------|---------|
| `true` | **Stateful** — maintains continuous conversation with dynamic message IDs (parent ID updated per turn) | ✅ |
| `false` | **Stateless** — each request branches from the initialization point (uses static `chatId` / `parentId` captured at startup or via `/new`) | |

### 4. Session Management

```bash
POST /new
```

Initializes a fresh chat session with a new Chat ID and Parent Message ID. Both the dynamic (history-mode) and static (stateless) IDs are reset to the new values.

## 🤖 Agent Mode

Agent Mode bridges OpenAI-style tool calling onto Kimi's single-prompt upstream. Enable it with `--agent-mode`.

### How It Works

1. **Prompt Flattening** — The OpenAI `messages[]` array is flattened into a single text prompt. Each non-user message is prefixed with a `[ROLE: <original_role>]` tag:
   - `[ROLE: system]`, `[ROLE: assistant]`, `[ROLE: developer]`
   - `[ROLE: tool_result]` for tool-role messages (with `tool_call_id` if present)
2. **Tool Contract** — The OpenAI `tools[]` array is rendered as a textual `[TOOL CONTRACT]` block appended to the prompt, describing each tool's name, description, and JSON-schema parameters.
3. **System Prefix** — A mandatory system preamble is injected, instructing the model to:
   - Emit tool calls as `<<<TOOL_CALL>>>{...}<<<END_TOOL_CALL>>>` blocks
   - Prefer user-provided tools over built-in Kimi tools when capabilities overlap
   - Use built-in Kimi tools (`web_search`, `ipython`, `show_widget`, etc.) to fill capability gaps
   - Stop immediately after `<<<END_TOOL_CALL>>>` and never narrate tool use in prose
4. **Output Interception** — A streaming state machine (`agentStreamInterceptor`) parses the model's output character-by-character:
   - Text outside tool-call blocks is passed through as standard content deltas
   - `<<<TOOL_CALL>>>` blocks are parsed and re-emitted as OpenAI-style `tool_calls` deltas (with `name` first, then streamed `arguments`)
   - A fallback extractor runs at stream end to catch any blocks the state machine missed
5. **Finish Reason** — `tool_calls` if any tool call was emitted; otherwise `stop`

### Tool Call Wire Format (Model Output)

The model is instructed to emit tool calls as:

```
<<<TOOL_CALL>>>
{"name":"<tool_name>","arguments":{"arg1":"value1"}}
<<<END_TOOL_CALL>>>
```

The proxy translates these into OpenAI-compatible `tool_calls` deltas:

```json
{
  "choices": [{
    "index": 0,
    "delta": {
      "tool_calls": [{
        "index": 0,
        "id": "call_abcd1234_0",
        "type": "function",
        "function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}
      }]
    },
    "finish_reason": null
  }]
}
```

### Example: Agent Mode with Tools

```bash
./kimi-proxy --agent-mode &
```

```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer Waguri" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "k2d6",
    "stream": false,
    "messages": [
      {"role": "user", "content": "What is the weather in Paris?"}
    ],
    "tools": [{
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get current weather for a city",
        "parameters": {
          "type": "object",
          "properties": {"city": {"type": "string"}},
          "required": ["city"]
        }
      }
    }]
  }'
```

Response:
```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_abcd1234_0",
        "type": "function",
        "function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}
      }]
    },
    "finish_reason": "tool_calls"
  }]
}
```

### Agent Mode Notes

- In Agent Mode, the OpenAI `search` field is **ignored** (the `tools` array is the canonical way to request tool use).
- Built-in Kimi tools are *not* enumerated in the contract — the model is told they exist and may invoke them via the same `<<<TOOL_CALL>>>` format.
- The interceptor detects content shrinkage (Kimi's edit/truncate behavior) and resets its state machine to avoid corrupt output.
- Token usage in non-stream responses is estimated as `(bytes + 3) / 4` — a rough heuristic, not a real tokenizer.

## 🛠️ Technical Details

### Performance Architecture

- **Connection Pooling**: `http.Transport` with `MaxIdleConns=500`, `MaxIdleConnsPerHost=100`, `IdleConnTimeout=90s`
- **HTTP/2**: Auto-negotiated with `ForceAttemptHTTP2`
- **Buffered I/O**: `bufio.NewReaderSize(64KB)` for upstream Connect-protocol frame parsing
- **Zero-Copy SSE**: Pre-allocated `sseDataPrefix`, `sseDataSuffix`, `sseDone` byte slices
- **Reusable Chunk Struct**: `openAIChunk` template reused across SSE frames
- **Thread-Safe State**: `sync.RWMutex` with read-lock for hot paths (`models`, `chatID`, `currentModelKey`)
- **Streaming-Friendly Server**: `WriteTimeout=0` (no write timeout — SSE streams can be long-lived), `ReadTimeout=30s`, `IdleTimeout=120s`

### Connect Protocol

Kimi uses the [Connect RPC](https://connect.build/) protocol for streaming:

```
Wire format: [1-byte flag] [4-byte BE length] [JSON payload]
  flag 0x00 = data frame
  flag 0x02 = error/trailer frame (logged & skipped)
```

Frames larger than 16 MB are rejected as a safety guard.

### State Management

- **Static IDs**: Captured on startup or via `POST /new`. Used when `history: false`.
- **Dynamic IDs**: Updated per-interaction when `history: true` (parent ID rolls forward after each assistant turn).
- **Model Context**: Per-request override via `model` field, or global default via `POST /models`.

### Request Flow

1. Decode & validate OpenAI request body
2. Verify server is initialized (chat session exists) — else `503`
3. Resolve model (per-request `model` or global default) — else `400`
4. Build prompt:
   - **Agent Mode**: `agentSystemPrefix` + role-tagged messages + optional `[TOOL CONTRACT]`
   - **Non-Agent Mode**: `"[role] text"` per message, joined by `\n\n`
5. Resolve `chat_id` / `parent_id` from history or static state
6. Encode payload in Connect wire format, POST to `kimiChatURL`
7. Parse Connect frames, extract `delta.content` / `block.text.content`
8. Emit OpenAI SSE chunks (with tool-call interception in Agent Mode) OR accumulate for non-stream response

### Headers Sent to Kimi Upstream

```
Accept: */*
Authorization: Bearer <KIMI_ACCESS_TOKEN>
Connect-Protocol-Version: 1
Content-Type: application/connect+json
R-Timezone: Asia/Calcutta
X-Language: en-US
X-Msh-Device-Id: <hardcoded>
X-Msh-Platform: web
X-Msh-Session-Id: <hardcoded>
X-Traffic-Id: <hardcoded>
Referer: https://www.kimi.com/chat/<chat_id>
```

## 🐛 Troubleshooting

| Issue | Solution |
|-------|----------|
| `401 Unauthorized` (from proxy) | Client must send `Authorization: Bearer <AUTH_KEY>` (default `Waguri`) |
| `401 Unauthorized` (from Kimi) | Verify `KIMI_ACCESS_TOKEN` is valid; regenerate from kimi.com |
| `503 Server not ready` | Server still initializing chat session — retry in a moment, or call `POST /new` |
| `502 Kimi API error` | Inspect logs for the upstream HTTP status and response body |
| `400 Invalid model` | Call `GET /models` to list valid keys; `POST /refresh-models` to update |
| Empty responses | Check `Authorization` header; verify model key exists |
| Streaming not working | Ensure client supports SSE; use `-N` flag in curl |
| Tool calls not emitted in Agent Mode | Model may have narrated intent instead of emitting the `<<<TOOL_CALL>>>` block. The fallback extractor at stream end will attempt to recover any complete blocks |
| Wrong parent message ID | Ensure `history: true` for continuous conversations; call `POST /new` to reset |

---
## 📄 License
This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
---
