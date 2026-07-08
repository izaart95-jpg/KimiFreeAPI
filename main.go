package main

import (
    "bufio"
    "bytes"
    "context"
    "crypto/rand"
    "encoding/binary"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "os/signal"
    "strings"
    "sync"
    "syscall"
    "time"
)

// ================= CONFIGURATION =================

var (
    port        = envOrDefault("PORT", "3000")
    accessToken = envOrDefault("KIMI_ACCESS_TOKEN", "")
    authKey     = "Bearer " + envOrDefault("AUTH_KEY", "Waguri")
)

const (
    kimiChatURL   = "https://www.kimi.com/apiv2/kimi.gateway.chat.v1.ChatService/Chat"
    kimiModelsURL = "https://www.kimi.com/apiv2/kimi.gateway.config.v1.ConfigService/GetAvailableModels"

    deviceID  = "7586915550627013133"
    sessionID = "1731469129988841572"
    trafficID = "d4t8j3es1rh1oljov7g0"
)

func envOrDefault(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

// ================= TYPES =================

type modelInfo struct {
    Scenario         string   `json:"scenario"`
    DisplayName      string   `json:"displayName"`
    Description      string   `json:"description"`
    InputPlaceholder string   `json:"inputPlaceholder,omitempty"`
    Key              string   `json:"key"`
    Thinking         bool     `json:"thinking,omitempty"`
    KimiPlusID       string   `json:"kimiPlusId,omitempty"`
    AgentMode        string   `json:"agentMode,omitempty"`
    SwitchableTo     []string `json:"switchableTo,omitempty"`
}

type modelsResponse struct {
    AvailableModels []modelInfo `json:"availableModels"`
    DefaultScenario struct {
        Scenario string `json:"scenario"`
    } `json:"defaultScenario"`
}

type openAIModel struct {
    ID      string `json:"id"`
    Name    string `json:"name,omitempty"`
    Created int64  `json:"created"`
    Object  string `json:"object"`
    OwnedBy string `json:"owned_by"`
}

type chatRequest struct {
    Messages  []chatMessage `json:"messages"`
    Model     string        `json:"model"`
    Stream    bool          `json:"stream"`
    DeepThink bool          `json:"deepThink"`
    Search    bool          `json:"search"`
}

type chatMessage struct {
    Role    string          `json:"role"`
    Content json.RawMessage `json:"content"`
}

// kimiFrame is a parsed Connect-protocol data frame from the Kimi upstream.
type kimiFrame struct {
    Chat *struct {
        ID string `json:"id"`
    } `json:"chat,omitempty"`
    Message *struct {
        ID string `json:"id"`
    } `json:"message,omitempty"`
    Delta *struct {
        Content string `json:"content"`
    } `json:"delta,omitempty"`
    Block *struct {
        Text *struct {
            Content string `json:"content"`
        } `json:"text,omitempty"`
    } `json:"block,omitempty"`
}

type openAIChunk struct {
    ID      string         `json:"id"`
    Object  string         `json:"object"`
    Created int64          `json:"created"`
    Model   string         `json:"model"`
    Choices []openAIChoice `json:"choices"`
}

type openAIChoice struct {
    Index        int     `json:"index"`
    Delta        delta   `json:"delta,omitempty"`
    FinishReason *string `json:"finish_reason"`
}

type delta struct {
    Content string `json:"content,omitempty"`
}

// ================= STATE =================

type appState struct {
    mu              sync.RWMutex
    chatID          string
    lastMessageID   string
    useHistory      bool
    currentModelKey string
    models          map[string]modelInfo
    modelsList      []openAIModel
    staticChatID    string
    staticParentID  string
}

var state = &appState{
    models: make(map[string]modelInfo),
    useHistory: true,
}

// ================= HTTP CLIENT (pooled, HTTP/2 capable) =================

var transport = &http.Transport{
    MaxIdleConns:          500,
    MaxIdleConnsPerHost:   100,
    IdleConnTimeout:       90 * time.Second,
    TLSHandshakeTimeout:   10 * time.Second,
    ResponseHeaderTimeout: 60 * time.Second,
    ForceAttemptHTTP2:     true,
}

var httpClient = &http.Client{
    Transport: transport,
}

// Pre-allocated SSE framing bytes — avoids per-write allocation.
var (
    sseDataPrefix = []byte("data: ")
    sseDataSuffix = []byte("\n\n")
    sseDone       = []byte("data: [DONE]\n\n")
)

// ================= HELPERS =================

func generateID() string {
    b := make([]byte, 16)
    rand.Read(b)
    return hex.EncodeToString(b)
}

// connectEncode serialises an object into Connect-protocol wire format:
//   [1-byte flag 0x00] [4-byte big-endian length] [JSON payload]
func connectEncode(v interface{}) ([]byte, error) {
    jsonBytes, err := json.Marshal(v)
    if err != nil {
        return nil, err
    }
    buf := make([]byte, 5+len(jsonBytes))
    buf[0] = 0x00
    binary.BigEndian.PutUint32(buf[1:5], uint32(len(jsonBytes)))
    copy(buf[5:], jsonBytes)
    return buf, nil
}

func sendJSON(w http.ResponseWriter, data interface{}, status int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(data)
}

func sendError(w http.ResponseWriter, message, errType string, code interface{}, status int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(map[string]interface{}{
        "error": map[string]interface{}{
            "message": message,
            "type":    errType,
            "param":   nil,
            "code":    code,
        },
    })
}

func isAuthenticated(r *http.Request) bool {
    return r.Header.Get("Authorization") == authKey
}

// extractPrompt pulls text from a chat message's content field.
// Handles plain strings and multimodal array content.
func extractPrompt(msg chatMessage) string {
    if len(msg.Content) == 0 {
        return ""
    }
    switch msg.Content[0] {
    case '"':
        var s string
        if json.Unmarshal(msg.Content, &s) == nil {
            return s
        }
    case '[':
        var parts []struct {
            Type string `json:"type"`
            Text string `json:"text"`
        }
        if json.Unmarshal(msg.Content, &parts) == nil {
            var texts []string
            for _, p := range parts {
                if p.Type == "text" {
                    texts = append(texts, p.Text)
                }
            }
            return strings.Join(texts, "\n")
        }
    }
    return ""
}

// ================= MODEL MANAGEMENT =================

func fetchModels() error {
    req, err := http.NewRequest("POST", kimiModelsURL, strings.NewReader("{}"))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-msh-platform", "web")

    resp, err := httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
        return fmt.Errorf("failed to fetch models: HTTP %d — %s", resp.StatusCode, string(body))
    }

    var mr modelsResponse
    if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
        return fmt.Errorf("failed to decode models response: %w", err)
    }

    state.mu.Lock()
    defer state.mu.Unlock()

    state.models = make(map[string]modelInfo, len(mr.AvailableModels))
    state.modelsList = make([]openAIModel, 0, len(mr.AvailableModels))
    now := time.Now().Unix()

    for _, m := range mr.AvailableModels {
        state.models[m.Key] = m
        state.modelsList = append(state.modelsList, openAIModel{
            ID:      m.Key,
            Name:    m.DisplayName,
            Created: now,
            Object:  "model",
            OwnedBy: "moonshot",
        })
    }

    // Pick a default model: first non-thinking model matching the default scenario
    if state.currentModelKey == "" {
        for _, m := range mr.AvailableModels {
            if m.Scenario == mr.DefaultScenario.Scenario && !m.Thinking {
                state.currentModelKey = m.Key
                break
            }
        }
        if state.currentModelKey == "" && len(mr.AvailableModels) > 0 {
            state.currentModelKey = mr.AvailableModels[0].Key
        }
    }

    return nil
}

// ================= KIMI UPSTREAM =================

func startNewChat() (chatID, lastMessageID string, err error) {
    state.mu.RLock()
    modelKey := state.currentModelKey
    model, ok := state.models[modelKey]
    state.mu.RUnlock()
    if !ok {
        return "", "", fmt.Errorf("model not found: %s", modelKey)
    }

    payload := map[string]interface{}{
        "scenario": model.Scenario,
        "tools":    []interface{}{},
        "message": map[string]interface{}{
            "role":     "user",
            "blocks":   []interface{}{map[string]interface{}{"message_id": "", "text": map[string]interface{}{"content": "Hello"}}},
            "scenario": model.Scenario,
        },
        "options": map[string]interface{}{"thinking": false},
    }

    postData, err := connectEncode(payload)
    if err != nil {
        return "", "", err
    }

    req, err := http.NewRequest("POST", kimiChatURL, bytes.NewReader(postData))
    if err != nil {
        return "", "", err
    }
    req.ContentLength = int64(len(postData))

    req.Header.Set("Accept", "*/*")
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Connect-Protocol-Version", "1")
    req.Header.Set("Content-Type", "application/connect+json")
    req.Header.Set("X-Msh-Device-Id", deviceID)
    req.Header.Set("X-Msh-Platform", "web")
    req.Header.Set("X-Msh-Session-Id", sessionID)
    req.Header.Set("Referer", "https://www.kimi.com/")

    resp, err := httpClient.Do(req)
    if err != nil {
        return "", "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
        return "", "", fmt.Errorf("kimi init error: HTTP %d — %s", resp.StatusCode, string(body))
    }

    reader := bufio.NewReaderSize(resp.Body, 64*1024)
    for {
        flag, err := reader.ReadByte()
        if err != nil {
            break
        }
        var lenBuf [4]byte
        if _, err := io.ReadFull(reader, lenBuf[:]); err != nil {
            break
        }
        length := binary.BigEndian.Uint32(lenBuf[:])
        if length == 0 {
            continue
        }
        if length > 16*1024*1024 {
            break
        }
        frame := make([]byte, length)
        if _, err := io.ReadFull(reader, frame); err != nil {
            break
        }

        if flag&0x02 != 0 { // error / trailer frame — skip
            continue
        }

        var data kimiFrame
        if json.Unmarshal(frame, &data) == nil {
            if data.Chat != nil && data.Chat.ID != "" {
                chatID = data.Chat.ID
            }
            if data.Message != nil && data.Message.ID != "" {
                lastMessageID = data.Message.ID
            }
        }
    }

    if chatID == "" {
        return "", "", fmt.Errorf("no chat ID returned from Kimi")
    }
    return chatID, lastMessageID, nil
}

// ================= HANDLERS =================

func handleHistory(w http.ResponseWriter, r *http.Request) {
    var enable bool

    if r.Method == http.MethodGet {
        q := r.URL.Query()
        enable = q.Get("enable") == "true" || q.Get("value") == "true"
    } else {
        var body struct {
            Enable bool `json:"enable"`
            Value  bool `json:"value"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
            sendError(w, "Invalid JSON", "invalid_request_error", "invalid_json", 400)
            return
        }
        enable = body.Enable || body.Value
    }

    state.mu.Lock()
    state.useHistory = enable
    chatID := state.chatID
    lastMsgID := state.lastMessageID
    state.mu.Unlock()

    resp := map[string]interface{}{
        "message":   fmt.Sprintf("History mode set to %v", enable),
        "useHistory": enable,
    }
    if r.Method == http.MethodGet {
        resp["currentIds"] = map[string]string{
            "chatId":        chatID,
            "lastMessageId": lastMsgID,
        }
    }
    sendJSON(w, resp, 200)
}

func handleNewChat(w http.ResponseWriter, r *http.Request) {
    log.Println("Starting new chat...")
    chatID, lastMessageID, err := startNewChat()
    if err != nil {
        log.Println("Error starting new chat:", err)
        sendError(w, err.Error(), "upstream_error", nil, 500)
        return
    }

    state.mu.Lock()
    state.chatID = chatID
    state.lastMessageID = lastMessageID
    state.staticChatID = chatID
    if lastMessageID != "" {
        state.staticParentID = lastMessageID
    }
    state.mu.Unlock()

    sendJSON(w, map[string]interface{}{
        "message":       "New chat started",
        "chatId":        chatID,
        "lastMessageId": lastMessageID,
    }, 200)
}

func handleModels(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        state.mu.RLock()
        models := state.modelsList
        state.mu.RUnlock()
        sendJSON(w, map[string]interface{}{
            "object": "list",
            "data":   models,
        }, 200)

    case http.MethodPost:
        var body struct {
            Model string `json:"model"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
            sendError(w, "Invalid JSON", "invalid_request_error", "invalid_json", 400)
            return
        }

        state.mu.Lock()
        if _, exists := state.models[body.Model]; !exists {
            available := make([]string, 0, len(state.models))
            for k := range state.models {
                available = append(available, k)
            }
            state.mu.Unlock()
            sendError(w,
                fmt.Sprintf("Invalid model ID. Available: %s", strings.Join(available, ", ")),
                "invalid_request_error", "model_not_found", 400)
            return
        }
        state.currentModelKey = body.Model
        current := state.currentModelKey
        state.mu.Unlock()

        sendJSON(w, map[string]interface{}{
            "message":      "Model updated",
            "currentModel": current,
        }, 201)

    default:
        sendError(w, "Method not allowed", "invalid_request_error", "method_not_allowed", 405)
    }
}

func handleRefreshModels(w http.ResponseWriter, r *http.Request) {
    if err := fetchModels(); err != nil {
        sendError(w, err.Error(), "upstream_error", nil, 502)
        return
    }
    state.mu.RLock()
    count := len(state.modelsList)
    current := state.currentModelKey
    state.mu.RUnlock()
    sendJSON(w, map[string]interface{}{
        "message":      "Models refreshed",
        "modelCount":   count,
        "currentModel": current,
    }, 200)
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
    var body chatRequest
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        sendError(w, "Invalid JSON", "invalid_request_error", "invalid_json", 400)
        return
    }

    // Verify server is initialised
    state.mu.RLock()
    chatID := state.chatID
    state.mu.RUnlock()
    if chatID == "" {
        sendError(w, "Server not ready. No Chat ID.", "server_error", nil, 503)
        return
    }

    // Resolve model: per-request override or global default
    modelKey := body.Model
    if modelKey == "" {
        state.mu.RLock()
        modelKey = state.currentModelKey
        state.mu.RUnlock()
    }

    state.mu.RLock()
    model, ok := state.models[modelKey]
    state.mu.RUnlock()
    if !ok {
        sendError(w, "Invalid model: "+modelKey, "invalid_request_error", "model_not_found", 400)
        return
    }

    // Extract prompt from the last message
    var prompt string
    if len(body.Messages) > 0 {
        prompt = extractPrompt(body.Messages[len(body.Messages)-1])
    }
    if prompt == "" {
        prompt = " "
    }

    // Resolve chat / parent IDs based on history mode
    state.mu.RLock()
    useHistory := state.useHistory
    var currentChatID, parentID string
    if useHistory {
        currentChatID = state.chatID
        parentID = state.lastMessageID
    } else {
        currentChatID = state.staticChatID
        parentID = state.staticParentID
    }
    state.mu.RUnlock()

    thinking := model.Thinking || body.DeepThink

    // Build Kimi Connect-protocol payload
    payload := map[string]interface{}{
        "chat_id":  currentChatID,
        "scenario": model.Scenario,
        "tools":    []interface{}{},
        "message": map[string]interface{}{
            "parent_id": parentID,
            "role":      "user",
            "blocks": []interface{}{
                map[string]interface{}{
                    "message_id": "",
                    "text":       map[string]interface{}{"content": prompt},
                },
            },
            "scenario": model.Scenario,
        },
        "options": map[string]interface{}{"thinking": thinking},
    }

    if body.Search {
        payload["tools"] = []interface{}{
            map[string]interface{}{"type": "TOOL_TYPE_SEARCH", "search": map[string]interface{}{}},
        }
    }
    if model.KimiPlusID != "" {
        payload["kimi_plus_id"] = model.KimiPlusID
    }

    postData, err := connectEncode(payload)
    if err != nil {
        sendError(w, err.Error(), "server_error", nil, 500)
        return
    }

    req, err := http.NewRequest("POST", kimiChatURL, bytes.NewReader(postData))
    if err != nil {
        sendError(w, err.Error(), "server_error", nil, 500)
        return
    }
    req.ContentLength = int64(len(postData))

    req.Header.Set("Accept", "*/*")
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Connect-Protocol-Version", "1")
    req.Header.Set("Content-Type", "application/connect+json")
    req.Header.Set("R-Timezone", "Asia/Calcutta")
    req.Header.Set("X-Language", "en-US")
    req.Header.Set("X-Msh-Device-Id", deviceID)
    req.Header.Set("X-Msh-Platform", "web")
    req.Header.Set("X-Msh-Session-Id", sessionID)
    req.Header.Set("X-Traffic-Id", trafficID)
    req.Header.Set("Referer", "https://www.kimi.com/chat/"+currentChatID)

    resp, err := httpClient.Do(req)
    if err != nil {
        sendError(w, err.Error(), "upstream_error", nil, 502)
        return
    }
    defer resp.Body.Close()

    log.Printf("Kimi API Status: %d  (model=%s  history=%v)", resp.StatusCode, modelKey, useHistory)

    if resp.StatusCode != http.StatusOK {
        respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
        sendError(w,
            fmt.Sprintf("Kimi API error: HTTP %d — %s", resp.StatusCode, string(respBody)),
            "upstream_error", nil, 502)
        return
    }

    // Begin SSE stream to client
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.WriteHeader(200)

    flusher, _ := w.(http.Flusher)

    // Reusable chunk template — avoids per-frame allocation
    chunk := openAIChunk{
        Object:  "chat.completion.chunk",
        Model:   "kimi",
        Choices: []openAIChoice{{Index: 0, FinishReason: nil}},
    }

    // Read Connect-protocol frames from upstream and relay as SSE
    reader := bufio.NewReaderSize(resp.Body, 64*1024)
    for {
        flag, err := reader.ReadByte()
        if err != nil {
            break
        }
        var lenBuf [4]byte
        if _, err := io.ReadFull(reader, lenBuf[:]); err != nil {
            break
        }
        length := binary.BigEndian.Uint32(lenBuf[:])
        if length == 0 {
            continue
        }
        if length > 16*1024*1024 {
            log.Printf("Frame too large: %d bytes — aborting stream", length)
            break
        }
        frame := make([]byte, length)
        if _, err := io.ReadFull(reader, frame); err != nil {
            break
        }

        // Skip Connect error / trailer frames (flag bit 1 set)
        if flag&0x02 != 0 {
            log.Printf("Kimi trailer frame: %s", string(frame))
            continue
        }

        var data kimiFrame
        if err := json.Unmarshal(frame, &data); err != nil {
            log.Println("Error parsing frame:", err)
            continue
        }

        // Track message ID for history mode
        if useHistory && data.Message != nil && data.Message.ID != "" {
            state.mu.Lock()
            state.lastMessageID = data.Message.ID
            state.mu.Unlock()
        }

        // Extract text content
        var content string
        if data.Delta != nil && data.Delta.Content != "" {
            content = data.Delta.Content
        } else if data.Block != nil && data.Block.Text != nil && data.Block.Text.Content != "" {
            content = data.Block.Text.Content
        }

        if content != "" {
            chunk.ID = "chatcmpl-" + generateID()
            chunk.Created = time.Now().Unix()
            chunk.Choices[0].Delta = delta{Content: content}

            chunkJSON, _ := json.Marshal(chunk)
            w.Write(sseDataPrefix)
            w.Write(chunkJSON)
            w.Write(sseDataSuffix)
            if flusher != nil {
                flusher.Flush()
            }
        }
    }

    // Terminate SSE stream
    w.Write(sseDone)
    if flusher != nil {
        flusher.Flush()
    }
}

// ================= MIDDLEWARE =================

func withMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // CORS
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

        if r.Method == http.MethodOptions {
            w.WriteHeader(200)
            return
        }

        // Auth
        if !isAuthenticated(r) {
            sendError(w, "Invalid or missing authentication token",
                "authentication_error", "invalid_api_key", 401)
            return
        }

        next(w, r)
    }
}

// ================= ROUTER =================

func router(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Path
    switch {
    case path == "/v1/chat/completions" && r.Method == http.MethodPost:
        handleChatCompletions(w, r)
    case path == "/history" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
        handleHistory(w, r)
    case path == "/new" && r.Method == http.MethodPost:
        handleNewChat(w, r)
    case path == "/models" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
        handleModels(w, r)
    case path == "/refresh-models" && r.Method == http.MethodPost:
        handleRefreshModels(w, r)
    default:
        sendError(w, "Not Found", "invalid_request_error", "not_found", 404)
    }
}

// ================= MAIN =================

func main() {
    // Fetch available models from Kimi server
    log.Println("Fetching available models from Kimi...")
    if err := fetchModels(); err != nil {
        log.Fatalf("Failed to fetch models: %v", err)
    }

    state.mu.RLock()
    log.Printf("Fetched %d models:", len(state.modelsList))
    for _, m := range state.modelsList {
        log.Printf("  • %s  (%s)", m.ID, m.Name)
    }
    log.Printf("Default model: %s", state.currentModelKey)
    state.mu.RUnlock()

    // Initialise chat session
    log.Println("Initializing... getting fresh Chat ID...")
    chatID, lastMessageID, err := startNewChat()
    if err != nil {
        log.Fatalf("Failed to initialize: %v", err)
    }

    state.mu.Lock()
    state.staticChatID = chatID
    state.staticParentID = lastMessageID
    state.chatID = chatID
    state.lastMessageID = lastMessageID
    state.mu.Unlock()

    log.Printf("Initialized with ChatID: %s", chatID)
    log.Printf("History mode default: %v", state.useHistory)

    // HTTP server tuned for streaming proxy workload
    server := &http.Server{
        Addr:         ":" + port,
        Handler:      withMiddleware(router),
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 0, // no timeout — SSE streams can be long-lived
        IdleTimeout:  120 * time.Second,
    }

    // Graceful shutdown
    go func() {
        sigChan := make(chan os.Signal, 1)
        signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
        <-sigChan
        log.Println("\nShutting down gracefully...")
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        if err := server.Shutdown(ctx); err != nil {
            log.Printf("Shutdown error: %v", err)
        }
    }()

    log.Printf("Kimi Proxy Server running on port %s", port)
    if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatalf("Server error: %v", err)
    }
}
