package main

import (
    "bufio"
    "bytes"
    "context"
    "crypto/rand"
    "encoding/binary"
    "encoding/hex"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    "os"
    "os/signal"
    "runtime"
    "strings"
    "sync"
    "syscall"
    "time"
)

// ==================== CONFIGURATION ====================

const (
    authToken   = "Waguri"
    kimiChatURL = "https://www.kimi.com/apiv2/kimi.gateway.chat.v1.ChatService/Chat"
    rTimezone   = "Asia/Calcutta"
    deviceID    = "7586915550627013133"
    sessionID   = "1731469129988841572"
    trafficID   = "d4t8j3es1rh1oljov7g0"
)

var accessToken string

// ==================== MODELS ====================

type Model struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Created int64  `json:"created"`
    Object  string `json:"object"`
    OwnedBy string `json:"owned_by"`
}

var availableModels = []Model{
    {ID: "SCENARIO_K2D5", Name: "Kimi 2.5", Created: 1700000000, Object: "model", OwnedBy: "moonshot"},
    {ID: "SCENARIO_K2D5_TURBO", Name: "Kimi 2.5 Turbo", Created: 1700000000, Object: "model", OwnedBy: "moonshot"},
}

// ==================== STATE ====================

type GlobalState struct {
    mu           sync.RWMutex
    chatID       string
    lastMsgID    string
    useHistory   bool
    currentModel string
    staticChatID string
    staticParent string
}

var state = &GlobalState{currentModel: "SCENARIO_K2D5"}

func (g *GlobalState) snapshot() (chatID, lastMsgID string, useHistory bool, model, staticChat, staticParent string) {
    g.mu.RLock()
    defer g.mu.RUnlock()
    return g.chatID, g.lastMsgID, g.useHistory, g.currentModel, g.staticChatID, g.staticParent
}

func (g *GlobalState) setHistory(v bool) {
    g.mu.Lock()
    g.useHistory = v
    g.mu.Unlock()
}

func (g *GlobalState) setModel(m string) {
    g.mu.Lock()
    g.currentModel = m
    g.mu.Unlock()
}

func (g *GlobalState) updateIDs(chatID, msgID string) {
    g.mu.Lock()
    g.chatID = chatID
    g.staticChatID = chatID
    if msgID != "" {
        g.lastMsgID = msgID
        g.staticParent = msgID
    }
    g.mu.Unlock()
}

func (g *GlobalState) setLastMsgID(id string) {
    g.mu.Lock()
    g.lastMsgID = id
    g.mu.Unlock()
}

// ==================== HTTP CLIENT ====================

var httpClient = &http.Client{
    Transport: &http.Transport{
        DialContext: (&net.Dialer{
            Timeout:   30 * time.Second,
            KeepAlive: 30 * time.Second,
        }).DialContext,
        ForceAttemptHTTP2:     true,
        MaxIdleConns:          200,
        MaxIdleConnsPerHost:   50,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 60 * time.Second,
        // Large read/write buffers per connection
        WriteBufferSize: 32 * 1024,
        ReadBufferSize:  32 * 1024,
    },
}

// ==================== BUFFER POOLS ====================

var bufPool = sync.Pool{
    New: func() any {
        return bytes.NewBuffer(make([]byte, 0, 32*1024))
    },
}

// ==================== TYPES ====================

type kimiRequest struct {
    ChatID   string           `json:"chat_id"`
    Scenario string           `json:"scenario"`
    Tools    []map[string]any `json:"tools"`
    Message  kimiMessage      `json:"message"`
    Options  kimiOptions      `json:"options"`
}

type kimiMessage struct {
    ParentID string      `json:"parent_id"`
    Role     string      `json:"role"`
    Blocks   []kimiBlock `json:"blocks"`
    Scenario string      `json:"scenario"`
}

type kimiBlock struct {
    MessageID string   `json:"message_id"`
    Text      kimiText `json:"text"`
}

type kimiText struct {
    Content string `json:"content"`
}

type kimiOptions struct {
    Thinking bool `json:"thinking"`
}

type kimiResponse struct {
    Chat *struct {
        ID string `json:"id"`
    } `json:"chat"`
    Message *struct {
        ID string `json:"id"`
    } `json:"message"`
    Delta *struct {
        Content string `json:"content"`
    } `json:"delta"`
    Block *struct {
        Text *struct {
            Content string `json:"content"`
        } `json:"text"`
    } `json:"block"`
}

type openAIChunk struct {
    ID      string         `json:"id"`
    Object  string         `json:"object"`
    Created int64          `json:"created"`
    Model   string         `json:"model"`
    Choices []openAIChoice `json:"choices"`
}

type openAIChoice struct {
    Index        int         `json:"index"`
    Delta        openAIDelta `json:"delta"`
    FinishReason *string     `json:"finish_reason"`
}

type openAIDelta struct {
    Content string `json:"content,omitempty"`
}

type errorResponse struct {
    Error errorBody `json:"error"`
}

type errorBody struct {
    Message string  `json:"message"`
    Type    string  `json:"type"`
    Param   *string `json:"param"`
    Code    *string `json:"code"`
}

type chatRequest struct {
    Messages  []map[string]any `json:"messages"`
    DeepThink bool             `json:"deepThink"`
    Search    bool             `json:"search"`
}

// ==================== HELPERS ====================

func generateID() string {
    b := make([]byte, 16)
    _, _ = rand.Read(b)
    return hex.EncodeToString(b)
}

func connectEncode(obj any) ([]byte, error) {
    payload, err := json.Marshal(obj)
    if err != nil {
        return nil, err
    }
    result := make([]byte, 5+len(payload))
    result[0] = 0x00
    binary.BigEndian.PutUint32(result[1:5], uint32(len(payload)))
    copy(result[5:], payload)
    return result, nil
}

func sendJSON(w http.ResponseWriter, data any, status int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(data)
}

func sendError(w http.ResponseWriter, message, errType string, code *string, status int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(errorResponse{
        Error: errorBody{
            Message: message,
            Type:    errType,
            Param:   nil,
            Code:    code,
        },
    })
}

func isAuthenticated(r *http.Request) bool {
    return r.Header.Get("Authorization") == "Bearer "+authToken
}

func strPtr(s string) *string { return &s }

// ==================== KIMI HEADERS ====================

func buildKimiHeaders(chatID string) http.Header {
    h := http.Header{}
    h.Set("accept", "*/*")
    h.Set("authorization", "Bearer "+accessToken)
    h.Set("connect-protocol-version", "1")
    h.Set("content-type", "application/connect+json")
    h.Set("r-timezone", rTimezone)
    h.Set("x-language", "en-US")
    h.Set("x-msh-device-id", deviceID)
    h.Set("x-msh-platform", "web")
    h.Set("x-msh-session-id", sessionID)
    h.Set("x-traffic-id", trafficID)
    h.Set("referer", "https://www.kimi.com/chat/"+chatID)
    return h
}

func buildKimiHeadersNewChat() http.Header {
    h := http.Header{}
    h.Set("accept", "*/*")
    h.Set("authorization", "Bearer "+accessToken)
    h.Set("connect-protocol-version", "1")
    h.Set("content-type", "application/connect+json")
    h.Set("x-msh-device-id", deviceID)
    h.Set("x-msh-platform", "web")
    h.Set("x-msh-session-id", sessionID)
    h.Set("referer", "https://www.kimi.com/")
    return h
}

// ==================== CONNECT FRAME PARSER ====================

// parseConnectFrames parses complete Connect-protocol frames from buf, invoking
// handler for each frame's JSON payload. Returns the remaining bytes.
func parseConnectFrames(buf *bytes.Buffer, handler func([]byte)) {
    for buf.Len() >= 5 {
        b := buf.Bytes()
        length := binary.BigEndian.Uint32(b[1:5])
        // Guard against absurd length values from malformed data
        if length > 64*1024*1024 {
            buf.Reset()
            return
        }
        if uint32(buf.Len()) < 5+length {
            return
        }
        handler(b[5 : 5+length])
        buf.Next(5 + int(length))
    }
}

// ==================== KIMI START NEW CHAT ====================

func startNewChat() (chatID, lastMsgID string, err error) {
    _, _, _, model, _, _ := state.snapshot()

    payload := kimiRequest{
        ChatID:   "",
        Scenario: model,
        Tools: []map[string]any{
            {"type": "TOOL_TYPE_SEARCH", "search": map[string]any{}},
        },
        Message: kimiMessage{
            Role:     "user",
            Scenario: model,
            Blocks: []kimiBlock{
                {MessageID: "", Text: kimiText{Content: "Hello"}},
            },
        },
        Options: kimiOptions{Thinking: false},
    }

    postData, err := connectEncode(payload)
    if err != nil {
        return "", "", err
    }

    req, err := http.NewRequest(http.MethodPost, kimiChatURL, bytes.NewReader(postData))
    if err != nil {
        return "", "", err
    }
    req.Header = buildKimiHeadersNewChat()

    resp, err := httpClient.Do(req)
    if err != nil {
        return "", "", err
    }
    defer resp.Body.Close()

    buf := bufPool.Get().(*bytes.Buffer)
    defer bufPool.Put(buf)
    buf.Reset()

    rdr := bufio.NewReaderSize(resp.Body, 32*1024)
    chunk := make([]byte, 32*1024)

    for {
        n, rerr := rdr.Read(chunk)
        if n > 0 {
            buf.Write(chunk[:n])
            parseConnectFrames(buf, func(frame []byte) {
                var data kimiResponse
                if err := json.Unmarshal(frame, &data); err == nil {
                    if data.Chat != nil && data.Chat.ID != "" {
                        chatID = data.Chat.ID
                    }
                    if data.Message != nil && data.Message.ID != "" {
                        lastMsgID = data.Message.ID
                    }
                }
            })
        }
        if rerr != nil {
            if rerr == io.EOF {
                break
            }
            return "", "", rerr
        }
    }

    if chatID == "" {
        return "", "", errors.New("no chat ID returned")
    }
    return chatID, lastMsgID, nil
}

// ==================== MIDDLEWARE ====================

func corsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        h := w.Header()
        h.Set("Access-Control-Allow-Origin", "*")
        h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
        if r.Method == http.MethodOptions {
            w.WriteHeader(http.StatusOK)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !isAuthenticated(r) {
            code := "invalid_api_key"
            sendError(w, "Invalid or missing authentication token", "authentication_error", &code, http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}

// ==================== HANDLERS ====================

func handleHistory(w http.ResponseWriter, r *http.Request) {
    chatID, lastMsgID, useHistory, _, _, _ := state.snapshot()

    switch r.Method {
    case http.MethodGet:
        q := r.URL.Query()
        enable := q.Get("enable") == "true" || q.Get("value") == "true"
        state.setHistory(enable)
        useHistory = enable
        sendJSON(w, map[string]any{
            "message":    fmt.Sprintf("History mode set to %v", useHistory),
            "useHistory": useHistory,
            "currentIds": map[string]string{
                "chatId":        chatID,
                "lastMessageId": lastMsgID,
            },
        }, http.StatusOK)

    case http.MethodPost:
        var body struct {
            Enable bool `json:"enable"`
            Value  bool `json:"value"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
            code := "invalid_json"
            sendError(w, "Invalid JSON", "invalid_request_error", &code, http.StatusBadRequest)
            return
        }
        enable := body.Enable || body.Value
        state.setHistory(enable)
        sendJSON(w, map[string]any{
            "message":    fmt.Sprintf("History mode set to %v", enable),
            "useHistory": enable,
        }, http.StatusOK)

    default:
        code := "method_not_allowed"
        sendError(w, "Method not allowed", "invalid_request_error", &code, http.StatusMethodNotAllowed)
    }
}

func handleNewChat(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        code := "method_not_allowed"
        sendError(w, "Method not allowed", "invalid_request_error", &code, http.StatusMethodNotAllowed)
        return
    }
    log.Println("Starting new chat...")
    chatID, lastMsgID, err := startNewChat()
    if err != nil {
        log.Println("Error starting new chat:", err)
        code := "upstream_error"
        sendError(w, err.Error(), "upstream_error", &code, http.StatusInternalServerError)
        return
    }
    state.updateIDs(chatID, lastMsgID)
    sendJSON(w, map[string]any{
        "message":       "New chat started",
        "chatId":        chatID,
        "lastMessageId": lastMsgID,
    }, http.StatusOK)
}

func handleModels(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        sendJSON(w, map[string]any{
            "object": "list",
            "data":   availableModels,
        }, http.StatusOK)

    case http.MethodPost:
        var body struct {
            Model string `json:"model"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
            code := "invalid_json"
            sendError(w, "Invalid JSON", "invalid_request_error", &code, http.StatusBadRequest)
            return
        }
        valid := false
        for _, m := range availableModels {
            if m.ID == body.Model {
                valid = true
                break
            }
        }
        if !valid {
            code := "model_not_found"
            sendError(w, "Invalid model ID. Must be SCENARIO_K2D5 or SCENARIO_K2D5_TURBO",
                "invalid_request_error", &code, http.StatusBadRequest)
            return
        }
        state.setModel(body.Model)
        _, _, _, currentModel, _, _ := state.snapshot()
        sendJSON(w, map[string]any{
            "message":      "Model updated",
            "currentModel": currentModel,
        }, http.StatusCreated)

    default:
        code := "method_not_allowed"
        sendError(w, "Method not allowed", "invalid_request_error", &code, http.StatusMethodNotAllowed)
    }
}

func extractPrompt(content any) string {
    switch v := content.(type) {
    case string:
        return v
    case []any:
        var sb strings.Builder
        for _, item := range v {
            if m, ok := item.(map[string]any); ok {
                if m["type"] == "text" {
                    if t, ok := m["text"].(string); ok {
                        if sb.Len() > 0 {
                            sb.WriteByte('\n')
                        }
                        sb.WriteString(t)
                    }
                }
            }
        }
        return sb.String()
    }
    return ""
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        code := "method_not_allowed"
        sendError(w, "Method not allowed", "invalid_request_error", &code, http.StatusMethodNotAllowed)
        return
    }

    // Limit body size to prevent abuse
    r.Body = http.MaxBytesReader(w, r.Body, 32<<20)

    var body chatRequest
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        code := "invalid_json"
        sendError(w, "Invalid JSON: "+err.Error(), "invalid_request_error", &code, http.StatusBadRequest)
        return
    }

    chatID, lastMsgID, useHistory, model, staticChat, staticParent := state.snapshot()

    if chatID == "" && staticChat == "" {
        code := "server_error"
        sendError(w, "Server not ready. No Chat ID.", "server_error", &code, http.StatusServiceUnavailable)
        return
    }

    var prompt string
    if len(body.Messages) > 0 {
        last := body.Messages[len(body.Messages)-1]
        prompt = extractPrompt(last["content"])
    }
    if prompt == "" {
        prompt = " "
    }

    var currentChatID, parentID string
    if useHistory {
        currentChatID = chatID
        parentID = lastMsgID
    } else {
        currentChatID = staticChat
        parentID = staticParent
    }

    log.Printf("Sending message using ChatID: %s, ParentID: %s, History: %v, Model: %s",
        currentChatID, parentID, useHistory, model)

    var tools []map[string]any
    if body.Search {
        tools = append(tools, map[string]any{
            "type":   "TOOL_TYPE_SEARCH",
            "search": map[string]any{},
        })
    }

    payload := kimiRequest{
        ChatID:   currentChatID,
        Scenario: model,
        Tools:    tools,
        Message: kimiMessage{
            ParentID: parentID,
            Role:     "user",
            Blocks: []kimiBlock{
                {MessageID: "", Text: kimiText{Content: prompt}},
            },
            Scenario: model,
        },
        Options: kimiOptions{Thinking: body.DeepThink},
    }

    postData, err := connectEncode(payload)
    if err != nil {
        code := "server_error"
        sendError(w, err.Error(), "server_error", &code, http.StatusInternalServerError)
        return
    }

    req, err := http.NewRequest(http.MethodPost, kimiChatURL, bytes.NewReader(postData))
    if err != nil {
        code := "server_error"
        sendError(w, err.Error(), "server_error", &code, http.StatusInternalServerError)
        return
    }
    req.Header = buildKimiHeaders(currentChatID)

    resp, err := httpClient.Do(req)
    if err != nil {
        code := "upstream_error"
        sendError(w, err.Error(), "upstream_error", &code, http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    log.Printf("Kimi API Status: %d", resp.StatusCode)

    // SSE headers
    h := w.Header()
    h.Set("Content-Type", "text/event-stream")
    h.Set("Cache-Control", "no-cache")
    h.Set("Connection", "keep-alive")
    h.Set("X-Accel-Buffering", "no") // Disable nginx buffering
    w.WriteHeader(http.StatusOK)

    flusher, _ := w.(http.Flusher)
    bw := bufio.NewWriterSize(w, 8192)
    enc := json.NewEncoder(bw)

    flush := func() {
        bw.Flush()
        if flusher != nil {
            flusher.Flush()
        }
    }

    // Reusable chunk object — minimal allocation per token
    chunkObj := openAIChunk{
        ID:      "chatcmpl-" + generateID(),
        Object:  "chat.completion.chunk",
        Created: time.Now().Unix(),
        Model:   "kimi",
        Choices: []openAIChoice{
            {Index: 0, Delta: openAIDelta{}, FinishReason: nil},
        },
    }

    buf := bufPool.Get().(*bytes.Buffer)
    defer bufPool.Put(buf)
    buf.Reset()

    rdr := bufio.NewReaderSize(resp.Body, 32*1024)
    readChunk := make([]byte, 32*1024)

    for {
        n, rerr := rdr.Read(readChunk)
        if n > 0 {
            buf.Write(readChunk[:n])
            parseConnectFrames(buf, func(frame []byte) {
                var data kimiResponse
                if err := json.Unmarshal(frame, &data); err != nil {
                    return
                }

                if useHistory && data.Message != nil && data.Message.ID != "" {
                    state.setLastMsgID(data.Message.ID)
                }

                var content string
                if data.Delta != nil && data.Delta.Content != "" {
                    content = data.Delta.Content
                } else if data.Block != nil && data.Block.Text != nil && data.Block.Text.Content != "" {
                    content = data.Block.Text.Content
                }

                if content != "" {
                    chunkObj.Choices[0].Delta.Content = content
                    bw.WriteString("data: ")
                    _ = enc.Encode(&chunkObj) // writes JSON + trailing \n
                    bw.WriteByte('\n')        // SSE needs \n\n
                    flush()
                }
            })
        }
        if rerr != nil {
            break
        }
    }

    bw.WriteString("data: [DONE]\n\n")
    flush()
}

// ==================== SERVER ====================

func main() {
    port := flag.Int("port", 3000, "Server port")
    token := flag.String("token", "", "Kimi access token")
    flag.Parse()

    accessToken = *token
    if accessToken == "" {
        accessToken = os.Getenv("KIMI_ACCESS_TOKEN")
    }
    if accessToken == "" {
        log.Fatal("Access token required: use -token flag or KIMI_ACCESS_TOKEN env var")
    }

    runtime.GOMAXPROCS(runtime.NumCPU())

    mux := http.NewServeMux()
    mux.HandleFunc("/history", handleHistory)
    mux.HandleFunc("/new", handleNewChat)
    mux.HandleFunc("/models", handleModels)
    mux.HandleFunc("/v1/chat/completions", handleChatCompletions)

    handler := corsMiddleware(authMiddleware(mux))

    srv := &http.Server{
        Addr:              fmt.Sprintf(":%d", *port),
        Handler:           handler,
        ReadHeaderTimeout: 10 * time.Second,
        ReadTimeout:       60 * time.Second,
        WriteTimeout:      0, // streaming responses need no write timeout
        IdleTimeout:       120 * time.Second,
        MaxHeaderBytes:    1 << 20,
    }

    // Graceful shutdown
    go func() {
        sigChan := make(chan os.Signal, 1)
        signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
        <-sigChan
        log.Println("Shutting down gracefully...")
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        _ = srv.Shutdown(ctx)
        os.Exit(0)
    }()

    // Initialize on startup
    log.Println("Initializing... getting fresh Chat ID...")
    chatID, lastMsgID, err := startNewChat()
    if err != nil {
        log.Fatal("Failed to initialize:", err)
    }
    state.updateIDs(chatID, lastMsgID)
    log.Printf("Initialized with ChatID: %s", chatID)

    log.Printf("Kimi Proxy Server (Go) running on port %d", *port)
    log.Printf("History mode default: %v", false)
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatal(err)
    }
}
