package main

import (
	"bytes"
	"encoding/json"
	jsonStr "encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bestk/kiro2cc/parser"
)

// TokenData defines the token file structure
type TokenData struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
}

// RefreshRequest defines the token refresh request payload
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// RefreshResponse defines the token refresh response payload
type RefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
}

// AnthropicTool defines the Anthropic API tool structure
type AnthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// InputSchema defines the tool input schema structure
type InputSchema struct {
	Json map[string]any `json:"json"`
}

// ToolSpecification defines the tool specification structure
type ToolSpecification struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// CodeWhispererTool defines the CodeWhisperer API tool structure
type CodeWhispererTool struct {
	ToolSpecification ToolSpecification `json:"toolSpecification"`
}

// HistoryUserMessage defines a user message in history
type HistoryUserMessage struct {
	UserInputMessage struct {
		Content string `json:"content"`
		ModelId string `json:"modelId"`
		Origin  string `json:"origin"`
	} `json:"userInputMessage"`
}

// HistoryAssistantMessage defines an assistant message in history
type HistoryAssistantMessage struct {
	AssistantResponseMessage struct {
		Content  string `json:"content"`
		ToolUses []any  `json:"toolUses"`
	} `json:"assistantResponseMessage"`
}

// AnthropicRequest defines the Anthropic API request structure
type AnthropicRequest struct {
	Model       string                    `json:"model"`
	MaxTokens   int                       `json:"max_tokens"`
	Messages    []AnthropicRequestMessage `json:"messages"`
	System      []AnthropicSystemMessage  `json:"system,omitempty"`
	Tools       []AnthropicTool           `json:"tools,omitempty"`
	Stream      bool                      `json:"stream"`
	Temperature *float64                  `json:"temperature,omitempty"`
	Metadata    map[string]any            `json:"metadata,omitempty"`
}

// AnthropicStreamResponse defines the Anthropic streaming response structure
type AnthropicStreamResponse struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentDelta struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"delta,omitempty"`
	Content []struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"content,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

// AnthropicRequestMessage defines the Anthropic API message structure
type AnthropicRequestMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // Can be string or []ContentBlock
}

type AnthropicSystemMessage struct {
	Type string `json:"type"`
	Text string `json:"text"` // Can be string or []ContentBlock
}

// ContentBlock defines the message content block structure
type ContentBlock struct {
	Type      string  `json:"type"`
	Text      *string `json:"text,omitempty"`
	ToolUseId *string `json:"tool_use_id,omitempty"`
	Content   *string `json:"content,omitempty"`
	Name      *string `json:"name,omitempty"`
	Input     *any    `json:"input,omitempty"`
}

// getMessageContent extracts text content from a message
func getMessageContent(content any) string {
	switch v := content.(type) {
	case string:
		if len(v) == 0 {
			return "answer for user qeustion"
		}
		return v
	case []interface{}:
		var texts []string
		for _, block := range v {

			if m, ok := block.(map[string]interface{}); ok {
				var cb ContentBlock
				if data, err := jsonStr.Marshal(m); err == nil {
					if err := jsonStr.Unmarshal(data, &cb); err == nil {
						switch cb.Type {
						case "tool_result":
							texts = append(texts, *cb.Content)
						case "text":
							texts = append(texts, *cb.Text)
						}
					}

				}
			}

		}
		if len(texts) == 0 {
			s, err := jsonStr.Marshal(content)
			if err != nil {
				return "answer for user qeustion"
			}

			log.Printf("uncatch: %s", string(s))
			return "answer for user qeustion"
		}
		return strings.Join(texts, "\n")
	default:
		s, err := jsonStr.Marshal(content)
		if err != nil {
			return "answer for user qeustion"
		}

		log.Printf("uncatch: %s", string(s))
		return "answer for user qeustion"
	}
}

// CodeWhispererRequest defines the CodeWhisperer API request structure
type CodeWhispererRequest struct {
	ConversationState struct {
		ChatTriggerType string `json:"chatTriggerType"`
		ConversationId  string `json:"conversationId"`
		CurrentMessage  struct {
			UserInputMessage struct {
				Content                 string `json:"content"`
				ModelId                 string `json:"modelId"`
				Origin                  string `json:"origin"`
				UserInputMessageContext struct {
					ToolResults []struct {
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
						Status    string `json:"status"`
						ToolUseId string `json:"toolUseId"`
					} `json:"toolResults,omitempty"`
					Tools []CodeWhispererTool `json:"tools,omitempty"`
				} `json:"userInputMessageContext"`
			} `json:"userInputMessage"`
		} `json:"currentMessage"`
		History []any `json:"history"`
	} `json:"conversationState"`
	ProfileArn string `json:"profileArn"`
}

// CodeWhispererEvent defines a CodeWhisperer event response
type CodeWhispererEvent struct {
	ContentType string `json:"content-type"`
	MessageType string `json:"message-type"`
	Content     string `json:"content"`
	EventType   string `json:"event-type"`
}

var ModelMap = map[string]string{
	"claude-sonnet-4-6":          "CLAUDE_SONNET_4_6_V1_0",
	"claude-sonnet-4-5-20250929": "CLAUDE_SONNET_4_5_20250929_V1_0",
	"claude-sonnet-4-20250514":   "CLAUDE_SONNET_4_6_V1_0",
	"claude-haiku-4-5-20251001":  "CLAUDE_HAIKU_4_5_20251001_V1_0",
	"claude-3-5-sonnet-20241022": "CLAUDE_SONNET_4_6_V1_0",
	"claude-3-5-haiku-20241022":  "CLAUDE_HAIKU_4_5_20251001_V1_0",
	"claude-3-7-sonnet-20250219": "CLAUDE_SONNET_4_6_V1_0",
}

var enforcedSystemPrompts = []string{
	"ultrathink",
	"think harder",
}

// generateUUID generates a simple UUID v4
func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant bits
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// buildCodeWhispererRequest builds a CodeWhisperer request
func buildCodeWhispererRequest(anthropicReq AnthropicRequest) CodeWhispererRequest {
	cwReq := CodeWhispererRequest{
		ProfileArn: "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK",
	}
	cwReq.ConversationState.ChatTriggerType = "MANUAL"
	cwReq.ConversationState.ConversationId = generateUUID()
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = getMessageContent(anthropicReq.Messages[len(anthropicReq.Messages)-1].Content)
	cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId = ModelMap[anthropicReq.Model]
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Origin = "AI_EDITOR"
	// Map tools information
	if len(anthropicReq.Tools) > 0 {
		var tools []CodeWhispererTool
		for _, tool := range anthropicReq.Tools {
			cwTool := CodeWhispererTool{}
			cwTool.ToolSpecification.Name = tool.Name
			cwTool.ToolSpecification.Description = tool.Description
			cwTool.ToolSpecification.InputSchema = InputSchema{
				Json: tool.InputSchema,
			}
			tools = append(tools, cwTool)
		}
		cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = tools
	}

	// Build history messages
	// Process system messages and regular history first
	if len(anthropicReq.System) > 0 || len(anthropicReq.Messages) > 1 || len(enforcedSystemPrompts) > 0 {
		var history []any

		// First add each system message as an independent history entry

		assistantDefaultMsg := HistoryAssistantMessage{}
		assistantDefaultMsg.AssistantResponseMessage.Content = getMessageContent("I will follow these instructions")
		assistantDefaultMsg.AssistantResponseMessage.ToolUses = make([]any, 0)

		// Always inject extra instruction prompts.
		for _, prompt := range enforcedSystemPrompts {
			userMsg := HistoryUserMessage{}
			userMsg.UserInputMessage.Content = prompt
			userMsg.UserInputMessage.ModelId = ModelMap[anthropicReq.Model]
			userMsg.UserInputMessage.Origin = "AI_EDITOR"
			history = append(history, userMsg)
			history = append(history, assistantDefaultMsg)
		}

		if len(anthropicReq.System) > 0 {
			for _, sysMsg := range anthropicReq.System {
				userMsg := HistoryUserMessage{}
				userMsg.UserInputMessage.Content = sysMsg.Text
				userMsg.UserInputMessage.ModelId = ModelMap[anthropicReq.Model]
				userMsg.UserInputMessage.Origin = "AI_EDITOR"
				history = append(history, userMsg)
				history = append(history, assistantDefaultMsg)
			}
		}

		// Then process regular message history
		for i := 0; i < len(anthropicReq.Messages)-1; i++ {
			if anthropicReq.Messages[i].Role == "user" {
				userMsg := HistoryUserMessage{}
				userMsg.UserInputMessage.Content = getMessageContent(anthropicReq.Messages[i].Content)
				userMsg.UserInputMessage.ModelId = ModelMap[anthropicReq.Model]
				userMsg.UserInputMessage.Origin = "AI_EDITOR"
				history = append(history, userMsg)

				// Check whether the next message is an assistant reply
				if i+1 < len(anthropicReq.Messages)-1 && anthropicReq.Messages[i+1].Role == "assistant" {
					assistantMsg := HistoryAssistantMessage{}
					assistantMsg.AssistantResponseMessage.Content = getMessageContent(anthropicReq.Messages[i+1].Content)
					assistantMsg.AssistantResponseMessage.ToolUses = make([]any, 0)
					history = append(history, assistantMsg)
					i++ // Skip the assistant message that was already processed
				}
			}
		}

		cwReq.ConversationState.History = history
	}

	return cwReq
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  kiro-claude-proxy read    - Read and display token")
		fmt.Println("  kiro-claude-proxy refresh - Refresh token")
		fmt.Println("  kiro-claude-proxy export  - Export environment variables")
		fmt.Println("  kiro-claude-proxy claude  - Skip Claude region restrictions")
		fmt.Println("  kiro-claude-proxy server [port] - Start Anthropic API proxy server")
		fmt.Println("  author https://github.com/bestK/kiro2cc")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "read":
		readToken()
	case "refresh":
		refreshToken()
	case "export":
		exportEnvVars()

	case "claude":
		setClaude()
	case "server":
		port := "8080" // Default port
		if len(os.Args) > 2 {
			port = os.Args[2]
		}
		startServer(port)
	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}

// getTokenFilePath returns the cross-platform token file path
func getTokenFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get user home directory: %v\n", err)
		os.Exit(1)
	}

	return filepath.Join(homeDir, ".aws", "sso", "cache", "kiro-auth-token.json")
}

// readToken reads and prints token information
func readToken() {
	tokenPath := getTokenFilePath()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		fmt.Printf("Failed to read token file: %v\n", err)
		os.Exit(1)
	}

	var token TokenData
	if err := jsonStr.Unmarshal(data, &token); err != nil {
		fmt.Printf("Failed to parse token file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Token Information:")
	fmt.Printf("Access Token: %s\n", token.AccessToken)
	fmt.Printf("Refresh Token: %s\n", token.RefreshToken)
	if token.ExpiresAt != "" {
		fmt.Printf("Expires at: %s\n", token.ExpiresAt)
	}
}

// refreshToken refreshes the token
func refreshToken() {
	tokenPath := getTokenFilePath()

	// Read current token
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		fmt.Printf("Failed to read token file: %v\n", err)
		os.Exit(1)
	}

	var currentToken TokenData
	if err := jsonStr.Unmarshal(data, &currentToken); err != nil {
		fmt.Printf("Failed to parse token file: %v\n", err)
		os.Exit(1)
	}

	// Prepare refresh request
	refreshReq := RefreshRequest{
		RefreshToken: currentToken.RefreshToken,
	}

	reqBody, err := jsonStr.Marshal(refreshReq)
	if err != nil {
		fmt.Printf("Failed to serialize request: %v\n", err)
		os.Exit(1)
	}

	// Send refresh request
	resp, err := http.Post(
		"https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken",
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		fmt.Printf("Failed to refresh token request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Failed to refresh token, status code: %d, response: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	// Parse response
	var refreshResp RefreshResponse
	if err := jsonStr.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
		fmt.Printf("Failed to parse refresh response: %v\n", err)
		os.Exit(1)
	}

	// Update token file
	newToken := TokenData(refreshResp)

	newData, err := jsonStr.MarshalIndent(newToken, "", "  ")
	if err != nil {
		fmt.Printf("Failed to serialize new token: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(tokenPath, newData, 0600); err != nil {
		fmt.Printf("Failed to write token file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Token refreshed successfully!")
	fmt.Printf("New Access Token: %s\n", newToken.AccessToken)
}

// exportEnvVars exports environment variables
func exportEnvVars() {
	tokenPath := getTokenFilePath()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		fmt.Printf("Failed to read token, please install Kiro and login first!: %v\n", err)
		os.Exit(1)
	}

	var token TokenData
	if err := jsonStr.Unmarshal(data, &token); err != nil {
		fmt.Printf("Failed to parse token file: %v\n", err)
		os.Exit(1)
	}

	// Output env var setup commands in OS-specific formats
	if runtime.GOOS == "windows" {
		fmt.Println("CMD")
		fmt.Printf("set ANTHROPIC_BASE_URL=http://localhost:8080\n")
		fmt.Printf("set ANTHROPIC_API_KEY=%s\n\n", token.AccessToken)
		fmt.Println("Powershell")
		fmt.Println(`$env:ANTHROPIC_BASE_URL="http://localhost:8080"`)
		fmt.Printf(`$env:ANTHROPIC_API_KEY="%s"`, token.AccessToken)
	} else {
		fmt.Printf("export ANTHROPIC_BASE_URL=http://localhost:8080\n")
		fmt.Printf("export ANTHROPIC_API_KEY=\"%s\"\n", token.AccessToken)
	}
}

func setClaude() {
	// C:\Users\WIN10\.claude.json
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get user home directory: %v\n", err)
		os.Exit(1)
	}

	claudeJsonPath := filepath.Join(homeDir, ".claude.json")
	ok, _ := FileExists(claudeJsonPath)
	if !ok {
		fmt.Println("Claude configuration file not found, please check if Claude Code is installed")
		fmt.Println("npm install -g @anthropic-ai/claude-code")
		os.Exit(1)
	}

	data, err := os.ReadFile(claudeJsonPath)
	if err != nil {
		fmt.Printf("Failed to read Claude file: %v\n", err)
		os.Exit(1)
	}

	var jsonData map[string]interface{}

	err = jsonStr.Unmarshal(data, &jsonData)

	if err != nil {
		fmt.Printf("Failed to parse JSON file: %v\n", err)
		os.Exit(1)
	}

	jsonData["hasCompletedOnboarding"] = true
	jsonData["kiro2cc"] = true

	newJson, err := json.MarshalIndent(jsonData, "", "  ")

	if err != nil {
		fmt.Printf("Failed to generate JSON file: %v\n", err)
		os.Exit(1)
	}

	err = os.WriteFile(claudeJsonPath, newJson, 0644)

	if err != nil {
		fmt.Printf("Failed to write JSON file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Claude configuration file updated")

}

// getToken gets the current token
func getToken() (TokenData, error) {
	tokenPath := getTokenFilePath()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return TokenData{}, fmt.Errorf("Failed to read token file: %v", err)
	}

	var token TokenData
	if err := jsonStr.Unmarshal(data, &token); err != nil {
		return TokenData{}, fmt.Errorf("Failed to parse token file: %v", err)
	}

	return token, nil
}

// logMiddleware logs all HTTP requests
func logMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// fmt.Printf("\n=== Request Received ===\n")
		// fmt.Printf("Time: %s\n", startTime.Format("2006-01-02 15:04:05"))
		// fmt.Printf("Request Method: %s\n", r.Method)
		// fmt.Printf("Request Path: %s\n", r.URL.Path)
		// fmt.Printf("Client IP: %s\n", r.RemoteAddr)
		// fmt.Printf("Headers:\n")
		// for name, values := range r.Header {
		// 	fmt.Printf("  %s: %s\n", name, strings.Join(values, ", "))
		// }

		// Call next handler
		next(w, r)

		// Measure processing duration
		duration := time.Since(startTime)
		fmt.Printf("Processing time: %v\n", duration)
		fmt.Printf("=== Request ended ===\n\n")
	}
}

// startServer starts the HTTP proxy server
func startServer(port string) {
	// Create router
	mux := http.NewServeMux()

	// Register all endpoints
	mux.HandleFunc("/v1/messages", logMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// Only handle POST requests
		if r.Method != http.MethodPost {
			fmt.Printf("Error: Unsupported request method\n")
			http.Error(w, "Only POST requests are supported", http.StatusMethodNotAllowed)
			return
		}

		// Get current token
		token, err := getToken()
		if err != nil {
			fmt.Printf("Error: Failed to get token: %v\n", err)
			http.Error(w, fmt.Sprintf("Failed to get token: %v", err), http.StatusInternalServerError)
			return
		}

		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Printf("Error: Failed to read request body: %v\n", err)
			http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		fmt.Printf("\n=========================Anthropic Request Body:\n%s\n=======================================\n", string(body))

		// Parse Anthropic request
		var anthropicReq AnthropicRequest
		if err := jsonStr.Unmarshal(body, &anthropicReq); err != nil {
			fmt.Printf("Error: Failed to parse request body: %v\n", err)
			http.Error(w, fmt.Sprintf("Failed to parse request body: %v", err), http.StatusBadRequest)
			return
		}

		// Basic validation with explicit error messages
		if anthropicReq.Model == "" {
			http.Error(w, `{"message":"Missing required field: model"}`, http.StatusBadRequest)
			return
		}
		if len(anthropicReq.Messages) == 0 {
			http.Error(w, `{"message":"Missing required field: messages"}`, http.StatusBadRequest)
			return
		}
		if _, ok := ModelMap[anthropicReq.Model]; !ok {
			// Return the list of available model names
			available := make([]string, 0, len(ModelMap))
			for k := range ModelMap {
				available = append(available, k)
			}
			errMsg := fmt.Sprintf("{\"message\":\"Unknown or unsupported model: %s\",\"availableModels\":[%s]}", anthropicReq.Model, "\""+strings.Join(available, "\",\"")+"\"")
			fmt.Printf("Error: Unknown model request: %s\n", anthropicReq.Model)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}

		// Handle streaming request
		if anthropicReq.Stream {
			handleStreamRequest(w, anthropicReq, token.AccessToken)
			return
		}

		// Handle non-streaming request
		handleNonStreamRequest(w, anthropicReq, token.AccessToken)
	}))

	// Add models endpoint
	mux.HandleFunc("/v1/models", logMiddleware(func(w http.ResponseWriter, r *http.Request) {
		type ModelEntry struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		}
		type ModelsResponse struct {
			Object string       `json:"object"`
			Data   []ModelEntry `json:"data"`
		}

		var data []ModelEntry
		for k := range ModelMap {
			data = append(data, ModelEntry{
				ID:      k,
				Object:  "model",
				Created: 1686960000,
				OwnedBy: "anthropic",
			})
		}

		w.Header().Set("Content-Type", "application/json")
		jsonStr.NewEncoder(w).Encode(ModelsResponse{
			Object: "list",
			Data:   data,
		})
	}))

	// Add health check endpoint
	mux.HandleFunc("/health", logMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// Add 404 handler
	mux.HandleFunc("/", logMiddleware(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Warning: unknown endpoint accessed\n")
		http.Error(w, "404 Not Found", http.StatusNotFound)
	}))

	// Start server
	fmt.Printf("Starting Anthropic API proxy server on port: %s\n", port)
	fmt.Printf("Available endpoints:\n")
	fmt.Printf("  POST /v1/messages - Anthropic API proxy\n")
	fmt.Printf("  GET  /v1/models   - List available models\n")
	fmt.Printf("  GET  /health      - Health check\n")
	fmt.Printf("Press Ctrl+C to stop the server\n")

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}
}

// handleStreamRequest handles streaming requests
func handleStreamRequest(w http.ResponseWriter, anthropicReq AnthropicRequest, accessToken string) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	messageId := fmt.Sprintf("msg_%s", time.Now().Format("20060102150405"))

	// Build CodeWhisperer request
	cwReq := buildCodeWhispererRequest(anthropicReq)

	// Serialize request body
	cwReqBody, err := jsonStr.Marshal(cwReq)
	if err != nil {
		sendErrorEvent(w, flusher, "Failed to serialize request", err)
		return
	}

	// fmt.Printf("CodeWhisperer streaming request body:\n%s\n", string(cwReqBody))

	// Create streaming proxy request
	proxyReq, err := http.NewRequest(
		http.MethodPost,
		"https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse",
		bytes.NewBuffer(cwReqBody),
	)
	if err != nil {
		sendErrorEvent(w, flusher, "Failed to create proxy request", err)
		return
	}

	// Set request headers
	proxyReq.Header.Set("Authorization", "Bearer "+accessToken)
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("Accept", "text/event-stream")

	// Send request
	client := &http.Client{}

	resp, err := client.Do(proxyReq)
	if err != nil {
		sendErrorEvent(w, flusher, "CodeWhisperer reqeust error", fmt.Errorf("reqeust error: %s", err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("CodeWhisperer response error, status code: %d, response: %s\n", resp.StatusCode, string(body))
		sendErrorEvent(w, flusher, "error", fmt.Errorf("Status code: %d", resp.StatusCode))

		if resp.StatusCode == 403 {
			refreshToken()
			sendErrorEvent(w, flusher, "error", fmt.Errorf("CodeWhisperer Token refreshed, please retry"))
		} else {
			sendErrorEvent(w, flusher, "error", fmt.Errorf("CodeWhisperer Error: %s ", string(body)))
		}
		return
	}

	// Read full response body first
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		sendErrorEvent(w, flusher, "error", fmt.Errorf("CodeWhisperer Error: Failed to read response"))
		return
	}

	// os.WriteFile(messageId+"response.raw", respBody, 0644)

	// Use the new CodeWhisperer parser
	events := parser.ParseEvents(respBody)

	if len(events) > 0 {

		// Send start events
		messageStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            messageId,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         anthropicReq.Model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  len(getMessageContent(anthropicReq.Messages[0].Content)),
					"output_tokens": 1,
				},
			},
		}
		sendSSEEvent(w, flusher, "message_start", messageStart)
		sendSSEEvent(w, flusher, "ping", map[string]string{
			"type": "ping",
		})

		contentBlockStart := map[string]any{
			"content_block": map[string]any{
				"text": "",
				"type": "text"},
			"index": 0, "type": "content_block_start",
		}

		sendSSEEvent(w, flusher, "content_block_start", contentBlockStart)
		// Process parsed events

		outputTokens := 0
		for _, e := range events {
			sendSSEEvent(w, flusher, e.Event, e.Data)

			if e.Event == "content_block_delta" {
				outputTokens = len(getMessageContent(e.Data))
			}

			// Random delay
			time.Sleep(time.Duration(rand.Intn(300)) * time.Millisecond)
		}

		contentBlockStop := map[string]any{
			"index": 0,
			"type":  "content_block_stop",
		}
		sendSSEEvent(w, flusher, "content_block_stop", contentBlockStop)

		contentBlockStopReason := map[string]any{
			"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{
				"output_tokens": outputTokens,
			},
		}
		sendSSEEvent(w, flusher, "message_delta", contentBlockStopReason)

		messageStop := map[string]any{
			"type": "message_stop",
		}
		sendSSEEvent(w, flusher, "message_stop", messageStop)
	}

}

// handleNonStreamRequest handles non-streaming requests
func handleNonStreamRequest(w http.ResponseWriter, anthropicReq AnthropicRequest, accessToken string) {
	// Build CodeWhisperer request
	cwReq := buildCodeWhispererRequest(anthropicReq)

	// Serialize request body
	cwReqBody, err := jsonStr.Marshal(cwReq)
	if err != nil {
		fmt.Printf("Error: Failed to serialize request: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to serialize request: %v", err), http.StatusInternalServerError)
		return
	}

	// fmt.Printf("CodeWhisperer request body:\n%s\n", string(cwReqBody))

	// Create proxy request
	proxyReq, err := http.NewRequest(
		http.MethodPost,
		"https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse",
		bytes.NewBuffer(cwReqBody),
	)
	if err != nil {
		fmt.Printf("Error: Failed to create proxy request: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to create proxy request: %v", err), http.StatusInternalServerError)
		return
	}

	// Set request headers
	proxyReq.Header.Set("Authorization", "Bearer "+accessToken)
	proxyReq.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{}

	resp, err := client.Do(proxyReq)
	if err != nil {
		fmt.Printf("Error: Failed to send request: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to send request: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Read response
	cwRespBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error: Failed to read response: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	// fmt.Printf("CodeWhisperer response body:\n%s\n", string(cwRespBody))

	respBodyStr := string(cwRespBody)

	events := parser.ParseEvents(cwRespBody)

	context := ""
	toolName := ""
	toolUseId := ""

	contexts := []map[string]any{}

	partialJsonStr := ""
	for _, event := range events {
		if event.Data != nil {
			if dataMap, ok := event.Data.(map[string]any); ok {
				switch dataMap["type"] {
				case "content_block_start":
					context = ""
				case "content_block_delta":
					if delta, ok := dataMap["delta"]; ok {

						if deltaMap, ok := delta.(map[string]any); ok {
							switch deltaMap["type"] {
							case "text_delta":
								if text, ok := deltaMap["text"]; ok {
									context += text.(string)
								}
							case "input_json_delta":
								toolUseId = deltaMap["id"].(string)
								toolName = deltaMap["name"].(string)
								if partial_json, ok := deltaMap["partial_json"]; ok {
									if strPtr, ok := partial_json.(*string); ok && strPtr != nil {
										partialJsonStr = partialJsonStr + *strPtr
									} else if str, ok := partial_json.(string); ok {
										partialJsonStr = partialJsonStr + str
									} else {
										log.Println("partial_json is not string or *string")
									}
								} else {
									log.Println("partial_json not found")
								}

							}
						}
					}

				case "content_block_stop":
					if index, ok := dataMap["index"]; ok {
						switch index {
						case 1:
							toolInput := map[string]interface{}{}
							if err := jsonStr.Unmarshal([]byte(partialJsonStr), &toolInput); err != nil {
								log.Printf("json unmarshal error:%s", err.Error())
							}

							contexts = append(contexts, map[string]interface{}{
								"type":  "tool_use",
								"id":    toolUseId,
								"name":  toolName,
								"input": toolInput,
							})
						case 0:
							contexts = append(contexts, map[string]interface{}{
								"text": context,
								"type": "text",
							})
						}
					}
				}

			}
		}
	}

	// Fallback: if text was accumulated without content_block_stop(index=0), still return text
	if len(contexts) == 0 && strings.TrimSpace(context) != "" {
		contexts = append(contexts, map[string]any{
			"type": "text",
			"text": context,
		})
	}
	
	// Check if response is an error
	if strings.Contains(string(cwRespBody), "Improperly formed request.") {
		fmt.Printf("Error: CodeWhisperer returned incorrect format: %s\n", respBodyStr)
		http.Error(w, fmt.Sprintf("Request format error: %s", respBodyStr), http.StatusBadRequest)
		return
	}

	// Build Anthropic response
	anthropicResp := map[string]any{
		"content":       contexts,
		"model":         anthropicReq.Model,
		"role":          "assistant",
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"type":          "message",
		"usage": map[string]any{
			"input_tokens":  len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Content),
			"output_tokens": len(context),
		},
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	jsonStr.NewEncoder(w).Encode(anthropicResp)
}

// sendSSEEvent sends an SSE event
func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) {

	json, err := jsonStr.Marshal(data)
	if err != nil {
		return
	}

	fmt.Printf("event: %s\n", eventType)
	fmt.Printf("data: %v\n\n", string(json))

	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", string(json))
	flusher.Flush()

}

// sendErrorEvent sends an error event
func sendErrorEvent(w http.ResponseWriter, flusher http.Flusher, message string, err error) {
	errorResp := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "overloaded_error",
			"message": message,
		},
	}

	// data: {"type": "error", "error": {"type": "overloaded_error", "message": "Overloaded"}}

	sendSSEEvent(w, flusher, "error", errorResp)
}

func FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil // File or directory exists
	}
	if os.IsNotExist(err) {
		return false, nil // File or directory does not exist
	}
	return false, err // Other error
}
