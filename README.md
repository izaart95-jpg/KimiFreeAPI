# Kimi Proxy Bridge

[![Node.js](https://img.shields.io/badge/Node.js-v16+-green.svg)](https://nodejs.org/)
[![API](https://img.shields.io/badge/API-OpenAI%20Compatible-orange.svg)]()

A lightweight, production-ready Node.js proxy server that provides an OpenAI-compatible API interface for Kimi AI (kimi.ai). Zero external dependencies, supporting streaming responses, multimodal inputs, and conversation history management.

## 📋 Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [API Reference](#api-reference)
- [Usage Examples](#usage-examples)
- [Technical Details](#technical-details)
- [Troubleshooting](#troubleshooting)

## ✨ Features

- **🔌 OpenAI-Compatible API** — Drop-in replacement for OpenAI chat completions endpoint
- **⚡ Zero Dependencies** — Uses native Node.js `http`/`https` modules only
- **🌊 Streaming Support** — Server-Sent Events (SSE) for real-time token streaming
- **🖼️ Multimodal Support** — Handles both text and structured content arrays
- **🧠 Extended Capabilities** — Deep thinking and web search functionality
- **📚 Flexible History** — Toggle between stateful conversations and stateless requests
- **🔄 Dynamic Model Switching** — Runtime model selection between K2.5 variants

## 📦 Prerequisites

- **Node.js**: Version 16.0 or higher recommended
- **Kimi.ai Account**: Valid authentication credentials required
- **Network Access**: Connectivity to kimi.ai services

## 🚀 Installation

1. **Clone or download** the repository
2. **Configure authentication** (see Configuration section)
3. **Start the server**:

```bash
node main.js
```

The server listens on **port 3000** by default and automatically initializes a new chat session on startup.

## 🔐 Configuration

### Obtaining Your Access Token

1. Navigate to [kimi.ai](https://kimi.ai) and log in to your account
2. Open **Developer Tools** (`F12` or `Ctrl+Shift+I`)
3. Navigate to **Application** tab (Chrome/Edge) or **Storage** (Firefox)
4. In the left sidebar, expand **Local Storage** → `https://kimi.ai`
5. Locate the `access_token` key and copy its value (JWT string starting with `eyJ...`)

**Alternative Method via Console:**
```javascript
localStorage.getItem('access_token')
```

### Server Configuration

Edit `main.js` and insert your token:

```javascript
// Configuration - Line ~10-15
const ACCESS_TOKEN = "your-access-token-here";
```

### Authentication Header

All API requests must include the Authorization header:

```http
Authorization: Bearer Waguri
```

*Note: The API key can be customized in the configuration file (around line 65).*

### Quick Connectivity Test

```bash
curl http://localhost:3000/models \
  -H "Authorization: Bearer Waguri"
```
## 🎥 Walkthrough

For detailed setup instructions and advanced usage scenarios, refer to the [video walkthrough](https://youtu.be/GlWP-YYddZg).

## 📡 API Reference

### 1. Chat Completions

**Endpoint:** `POST /v1/chat/completions`

Creates a chat completion for the provided messages. Compatible with OpenAI's chat completions specification.

#### Request Headers
| Header | Value |
|--------|-------|
| `Authorization` | `Bearer Waguri` |
| `Content-Type` | `application/json` |

#### Request Body
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `messages` | Array | Yes | Conversation history array |
| `model` | String | Yes | Model identifier: `SCENARIO_K2D5` or `SCENARIO_K2D5_TURBO` |
| `stream` | Boolean | No | Enable SSE streaming (default: `false`) |
| `deepThink` | Boolean | No | Enable enhanced reasoning mode |
| `search` | Boolean | No | Enable web search capabilities |

#### Message Format
```json
{
  "role": "user",
  "content": "Your message here"
}
```

**Multimodal Content Format:**
```json
{
  "role": "user",
  "content": [
    { "type": "text", "text": "Analyze this image" }
  ]
}
```

### 2. History Mode Management

**Endpoint:** `GET /history` or `POST /history`

Controls conversation context persistence.

| Mode | Behavior |
|------|----------|
| `false` (Default) | Stateless mode. Uses static IDs; each request branches from initialization point |
| `true` | Stateful mode. Maintains continuous conversation context with dynamic message IDs |

**POST Example:**
```bash
curl -X POST http://localhost:3000/history \
  -H "Authorization: Bearer Waguri" \
  -H "Content-Type: application/json" \
  -d '{"enable": true}'
```

**GET Example:**
```bash
curl "http://localhost:3000/history?enable=true" \
  -H "Authorization: Bearer Waguri"
```

### 3. Session Management

**Endpoint:** `POST /new`

Initializes a fresh chat session, generating new Chat ID and Parent Message ID. Resets both static and global state.

**Response:**
```json
{
  "message": "New chat started",
  "chatId": "...",
  "lastMessageId": "..."
}
```

### 4. Model Management

**List Available Models**
```bash
GET /models
```

**Switch Active Model**
```bash
POST /models
Content-Type: application/json

{
  "model": "SCENARIO_K2D5_TURBO"
}
```

**Available Models:**
- `SCENARIO_K2D5` — Kimi 2.5 Standard
- `SCENARIO_K2D5_TURBO` — Kimi 2.5 Turbo (optimized for speed)

## 💡 Usage Examples

### Basic Completion (Non-Streaming)
```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer Waguri" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      { "role": "user", "content": "Hello! How are you?" }
    ],
    "model": "SCENARIO_K2D5",
    "stream": false
  }'
```

### Streaming Response
```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer Waguri" \
  -H "Content-Type: application/json" \
  -N \
  -d '{
    "messages": [
      { "role": "user", "content": "Tell me a short story" }
    ],
    "model": "SCENARIO_K2D5",
    "stream": true
  }'
```

### Deep Thinking Mode
```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer Waguri" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      { "role": "user", "content": "Solve this complex math problem: 2x + 5 = 15" }
    ],
    "model": "SCENARIO_K2D5",
    "deepThink": true,
    "stream": false
  }'
```

### Web Search Enabled
```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer Waguri" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      { "role": "user", "content": "What are the latest developments in AI?" }
    ],
    "model": "SCENARIO_K2D5",
    "search": true,
    "stream": false
  }'
```

## 🛠️ Technical Details

### State Management Architecture

The server maintains an internal `globalState` object:

- **Static IDs**: Persistent identifiers created on startup or via `/new`. Used when `history: false`.
- **Dynamic IDs**: Updated per-interaction when `history: true` to maintain conversation continuity.
- **Model Context**: Globally switched for all subsequent requests when changed via `/models`.

### Error Handling

- Returns standardized OpenAI-format error objects
- Global `uncaughtException` handler prevents server crashes from malformed payloads
- Automatic retry logic for transient network failures

### Content Processing

- **Text Normalization**: Automatically extracts text from array-based content structures
- **Agent Compatibility**: Safe for integration with LangChain, AutoGen, and other agent frameworks
- **Encoding**: UTF-8 support for international character sets


## 🐛 Troubleshooting

| Issue | Solution |
|-------|----------|
| `401 Unauthorized` | Verify `access_token` is valid and not expired; regenerate from kimi.ai |
| `Connection refused` | Ensure Node.js server is running on port 3000 (or configured port) |
| Empty responses | Check that `Authorization` header exactly matches `Bearer Waguri` |
| Streaming not working | Ensure client supports SSE and `-N` flag is used in curl |
| Model errors | Verify model identifier is exactly `SCENARIO_K2D5` or `SCENARIO_K2D5_TURBO` |

---

**Note**: This is an unofficial community project and is not affiliated with Moonshot AI or kimi.ai.
