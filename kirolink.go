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
	"sort"
	"strings"
	"time"

	"github.com/alexandeism/kirolink/protocol"
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

// getMessageContent extracts text content from a message, handling latest block types
func getMessageContent(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		if len(v) == 0 {
			return ""
		}
		return v
	case []interface{}:
		var texts []string
		for _, block := range v {
			if m, ok := block.(map[string]interface{}); ok {
				blockType, _ := m["type"].(string)
				switch blockType {
				case "text":
					if text, ok := m["text"].(string); ok {
						texts = append(texts, text)
					}
				case "thought": // 2025+ Thinking feature
					if thought, ok := m["thought"].(string); ok {
						texts = append(texts, fmt.Sprintf("[Thinking: %s]", thought))
					}
				case "tool_result":
					// tool_result content can be a string, array, or nil (e.g. exa deep research pending)
					rawContent, hasContent := m["content"]
					if !hasContent || rawContent == nil {
						// No content yet (async tool still running)
						if toolUseId, ok := m["tool_use_id"].(string); ok {
							texts = append(texts, fmt.Sprintf("[Tool result pending: %s]", toolUseId))
						}
						break
					}
					switch c := rawContent.(type) {
					case string:
						texts = append(texts, c)
					case []interface{}:
						for _, item := range c {
							if itemMap, ok := item.(map[string]interface{}); ok {
								itemType, _ := itemMap["type"].(string)
								if itemType == "text" {
									if text, ok := itemMap["text"].(string); ok {
										texts = append(texts, text)
									}
								} else {
									// Support for images/other types in results as strings
									if data, err := jsonStr.Marshal(itemMap); err == nil {
										texts = append(texts, string(data))
									}
								}
							}
						}
					}
				case "tool_use":
					// Include tool info as XML metadata — NOT as a mimicable text pattern.
					// Using "[Tool call: name(args)]" caused the model to output tool calls
					// as text instead of using structured tool calling. XML tags give context
					// without creating a pattern the model copies.
					name, _ := m["name"].(string)
					id, _ := m["id"].(string)
					if name != "" {
						texts = append(texts, fmt.Sprintf("<tool_executed name=%q id=%q />", name, id))
					}
				case "tool_search": // 2026 agentic feature
					if query, ok := m["query"].(string); ok {
						texts = append(texts, fmt.Sprintf("[Tool search: %s]", query))
					}
				default:
					// Fallback for unknown block types
					if data, err := jsonStr.Marshal(m); err == nil {
						texts = append(texts, string(data))
					}
				}
			}
		}
		if len(texts) == 0 {
			if s, err := jsonStr.Marshal(content); err == nil {
				return string(s)
			}
			return ""
		}
		return strings.Join(texts, "\n")
	default:
		s, err := jsonStr.Marshal(content)
		if err != nil {
			return ""
		}
		return string(s)
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

const (
	modelSonnet46 = "CLAUDE_SONNET_4_6_V1_0"
	modelSonnet45 = "CLAUDE_SONNET_4_5_20250929_V1_0"
	modelOpus46   = "CLAUDE_OPUS_4_6_V1_0"
	modelHaiku45  = "CLAUDE_HAIKU_4_5_20251001_V1_0"

	// Payload safety limits for CodeWhisperer
	maxToolDescLen  = 200    // max characters per tool description
	maxPayloadBytes = 250000 // ~250KB soft limit for total request JSON
)

var ModelMap = map[string]string{
	"default":                    modelSonnet46,
	"claude-sonnet-4-6":          modelSonnet46,
	"claude-sonnet-4-5":          modelSonnet45,
	"claude-sonnet-4-5-20250929": modelSonnet45,
	"claude-sonnet-4-20250514":   modelSonnet46,
	"claude-opus-4-6":            modelOpus46,
	"claude-haiku-4-5-20251001":  modelHaiku45,
	"claude-3-5-sonnet-20241022": modelSonnet46,
	"claude-3-5-haiku-20241022":  modelHaiku45,
	"claude-3-7-sonnet-20250219": modelSonnet46,
	"claude-3-7-haiku-20250219":  modelHaiku45,
	"claude-4-sonnet":            modelSonnet46,
	"claude-4-haiku":             modelHaiku45,
	"claude-4-opus":              modelOpus46,
}

func resolveModelID(requested string) string {
	key := strings.ToLower(strings.TrimSpace(requested))
	if key == "" {
		return modelSonnet46
	}
	if v, ok := ModelMap[key]; ok {
		return v
	}

	// Accept direct provider IDs.
	if strings.HasPrefix(key, "claude_") {
		return strings.ToUpper(key)
	}

	// Handle loose UI labels / aliases from ACP clients.
	switch {
	case strings.Contains(key, "default"):
		return modelSonnet46
	case strings.Contains(key, "sonnet") && strings.Contains(key, "4-5"):
		return modelSonnet45
	case strings.Contains(key, "sonnet") && strings.Contains(key, "4.5"):
		return modelSonnet45
	case strings.Contains(key, "sonnet"):
		return modelSonnet46
	case strings.Contains(key, "opus"):
		return modelOpus46
	case strings.Contains(key, "haiku"):
		return modelHaiku45
	default:
		// Safe default keeps Obsidian sessions working if it sends unknown aliases.
		return modelSonnet46
	}
}

// generateUUID generates a simple UUID v4
func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant bits
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// truncateString truncates a string to maxLen, appending "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// simplifySchema recursively simplifies a JSON schema to reduce payload size.
// It keeps property names, types, required fields, and enum values but strips
// verbose nested descriptions, examples, and deeply nested sub-schemas.
func simplifySchema(schema map[string]any, depth int) map[string]any {
	if depth > 3 {
		// Beyond depth 3, collapse to just the type
		if t, ok := schema["type"]; ok {
			return map[string]any{"type": t}
		}
		return map[string]any{"type": "object"}
	}

	result := make(map[string]any)

	// Always keep these keys
	for _, key := range []string{"type", "required", "enum", "const", "additionalProperties"} {
		if v, ok := schema[key]; ok {
			result[key] = v
		}
	}

	// Simplify "properties" recursively
	if props, ok := schema["properties"]; ok {
		if propsMap, ok := props.(map[string]any); ok {
			simplifiedProps := make(map[string]any)
			for propName, propVal := range propsMap {
				if propSchema, ok := propVal.(map[string]any); ok {
					simplifiedProps[propName] = simplifySchema(propSchema, depth+1)
				} else {
					simplifiedProps[propName] = propVal
				}
			}
			result["properties"] = simplifiedProps
		}
	}

	// Simplify "items" for arrays
	if items, ok := schema["items"]; ok {
		if itemsMap, ok := items.(map[string]any); ok {
			result["items"] = simplifySchema(itemsMap, depth+1)
		} else {
			result["items"] = items
		}
	}

	// Simplify "anyOf" / "oneOf" / "allOf"
	for _, combiner := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := schema[combiner]; ok {
			if arrSlice, ok := arr.([]any); ok {
				var simplified []any
				for _, item := range arrSlice {
					if itemMap, ok := item.(map[string]any); ok {
						simplified = append(simplified, simplifySchema(itemMap, depth+1))
					} else {
						simplified = append(simplified, item)
					}
				}
				result[combiner] = simplified
			}
		}
	}

	return result
}

// buildCodeWhispererTools converts Anthropic tools to CodeWhisperer format with
// truncation and schema simplification to keep the payload within safe limits.
func buildCodeWhispererTools(tools []AnthropicTool) []CodeWhispererTool {
	var cwTools []CodeWhispererTool
	for _, tool := range tools {
		cwTool := CodeWhispererTool{}
		cwTool.ToolSpecification.Name = tool.Name
		cwTool.ToolSpecification.Description = truncateString(tool.Description, maxToolDescLen)
		cwTool.ToolSpecification.InputSchema = InputSchema{
			Json: simplifySchema(tool.InputSchema, 0),
		}
		cwTools = append(cwTools, cwTool)
	}
	return cwTools
}

// ensurePayloadFits serializes the request and if it exceeds maxPayloadBytes,
// progressively trims history (oldest first) and further truncates tool
// descriptions until it fits. Returns the final serialized JSON.
func ensurePayloadFits(cwReq *CodeWhispererRequest) ([]byte, error) {
	data, err := jsonStr.Marshal(cwReq)
	if err != nil {
		return nil, err
	}

	// Fast path: already fits
	if len(data) <= maxPayloadBytes {
		return data, nil
	}

	fmt.Printf("[payload-trim] initial size %d bytes, limit %d\n", len(data), maxPayloadBytes)

	// Phase 1: Trim history from the front (oldest caller context first)
	for len(data) > maxPayloadBytes && len(cwReq.ConversationState.History) > 0 {
		cwReq.ConversationState.History = trimOldestHistoryMessage(cwReq.ConversationState.History)
		data, err = jsonStr.Marshal(cwReq)
		if err != nil {
			return nil, err
		}
	}

	if len(data) <= maxPayloadBytes {
		fmt.Printf("[payload-trim] fit after history trim: %d bytes\n", len(data))
		return data, nil
	}

	// Phase 2: Further truncate tool descriptions to 100 chars
	tools := cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools
	for i := range tools {
		tools[i].ToolSpecification.Description = truncateString(tools[i].ToolSpecification.Description, 100)
	}
	data, err = jsonStr.Marshal(cwReq)
	if err != nil {
		return nil, err
	}

	if len(data) <= maxPayloadBytes {
		fmt.Printf("[payload-trim] fit after desc trim: %d bytes\n", len(data))
		return data, nil
	}

	// Phase 3: Strip tool schemas entirely (keep only name + short description)
	for i := range tools {
		tools[i].ToolSpecification.InputSchema = InputSchema{Json: map[string]any{"type": "object"}}
	}
	data, err = jsonStr.Marshal(cwReq)
	if err != nil {
		return nil, err
	}

	if len(data) <= maxPayloadBytes {
		fmt.Printf("[payload-trim] fit after schema strip: %d bytes\n", len(data))
		return data, nil
	}

	// Phase 4: Drop tools entirely as last resort
	cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = nil
	data, err = jsonStr.Marshal(cwReq)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[payload-trim] dropped all tools, final size: %d bytes\n", len(data))
	return data, nil
}

func trimOldestHistoryMessage(history []any) []any {
	if len(history) == 0 {
		return history
	}
	return history[1:]
}

func keepMostRecentHistory(history []any, keep int) []any {
	if keep <= 0 || len(history) == 0 {
		return nil
	}
	if len(history) <= keep {
		return history
	}
	return append([]any(nil), history[len(history)-keep:]...)
}

func buildSystemContext(system []AnthropicSystemMessage) string {
	parts := make([]string, 0, len(system))
	for _, sysMsg := range system {
		if text := strings.TrimSpace(sysMsg.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func buildCurrentMessageContent(anthropicReq AnthropicRequest) string {
	lastMsg := anthropicReq.Messages[len(anthropicReq.Messages)-1]
	parts := make([]string, 0, 2)
	if systemContext := buildSystemContext(anthropicReq.System); systemContext != "" {
		parts = append(parts, fmt.Sprintf("<context>\n%s\n</context>", systemContext))
	}
	parts = append(parts, fmt.Sprintf("<task>\n%s\n</task>", getMessageContent(lastMsg.Content)))
	return strings.Join(parts, "\n\n")
}

// extractToolResults extracts tool_result blocks from an Anthropic message content
// and returns them in CodeWhisperer toolResults format.
func extractToolResults(content any) []struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Status    string `json:"status"`
	ToolUseId string `json:"toolUseId"`
} {
	type cwToolResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Status    string `json:"status"`
		ToolUseId string `json:"toolUseId"`
	}

	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}

	var results []cwToolResult
	for _, block := range blocks {
		m, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := m["type"].(string)
		if blockType != "tool_result" {
			continue
		}

		toolUseId, _ := m["tool_use_id"].(string)
		if toolUseId == "" {
			continue
		}

		status := "success"
		if isErr, ok := m["is_error"].(bool); ok && isErr {
			status = "error"
		}

		// Extract text content from tool_result
		var textBlocks []struct {
			Text string `json:"text"`
		}
		rawContent, hasContent := m["content"]
		if !hasContent || rawContent == nil {
			textBlocks = append(textBlocks, struct {
				Text string `json:"text"`
			}{Text: ""})
		} else {
			switch c := rawContent.(type) {
			case string:
				textBlocks = append(textBlocks, struct {
					Text string `json:"text"`
				}{Text: c})
			case []interface{}:
				for _, item := range c {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if text, ok := itemMap["text"].(string); ok {
							textBlocks = append(textBlocks, struct {
								Text string `json:"text"`
							}{Text: text})
						} else {
							if data, err := jsonStr.Marshal(itemMap); err == nil {
								textBlocks = append(textBlocks, struct {
									Text string `json:"text"`
								}{Text: string(data)})
							}
						}
					}
				}
			default:
				if data, err := jsonStr.Marshal(rawContent); err == nil {
					textBlocks = append(textBlocks, struct {
						Text string `json:"text"`
					}{Text: string(data)})
				}
			}
		}

		results = append(results, cwToolResult{
			Content:   textBlocks,
			Status:    status,
			ToolUseId: toolUseId,
		})
	}

	// Return type matches the struct definition in CodeWhispererRequest
	type ret = struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Status    string `json:"status"`
		ToolUseId string `json:"toolUseId"`
	}
	var out []ret
	for _, r := range results {
		out = append(out, ret(r))
	}
	return out
}

// extractToolUses pulls tool_use blocks from an Anthropic assistant message content
// and returns them for the CodeWhisperer history toolUses field.
func extractToolUses(content any) []any {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}

	var toolUses []any
	for _, block := range blocks {
		m, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := m["type"].(string)
		if blockType != "tool_use" {
			continue
		}

		toolUseId, _ := m["id"].(string)
		name, _ := m["name"].(string)
		input := m["input"]

		if toolUseId != "" && name != "" {
			inputObj := input
			if inputObj == nil {
				inputObj = map[string]any{}
			}
			toolUses = append(toolUses, map[string]any{
				"toolUseId": toolUseId,
				"name":      name,
				"input":     inputObj,
			})
		}
	}
	return toolUses
}

// hasToolResults checks if a message content contains tool_result blocks.
func hasToolResults(content any) bool {
	blocks, ok := content.([]interface{})
	if !ok {
		return false
	}
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok {
			if t, _ := m["type"].(string); t == "tool_result" {
				return true
			}
		}
	}
	return false
}

// buildCodeWhispererRequest builds a CodeWhisperer request
func buildCodeWhispererRequest(anthropicReq AnthropicRequest) CodeWhispererRequest {
	cwReq := CodeWhispererRequest{
		ProfileArn: "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK",
	}
	resolvedModel := resolveModelID(anthropicReq.Model)
	cwReq.ConversationState.ChatTriggerType = "MANUAL"
	cwReq.ConversationState.ConversationId = generateUUID()
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = buildCurrentMessageContent(anthropicReq)
	cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId = resolvedModel
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Origin = "AI_EDITOR"

	// Map tools information with truncation and schema simplification
	if len(anthropicReq.Tools) > 0 {
		cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = buildCodeWhispererTools(anthropicReq.Tools)
	}

	// NOTE: We do NOT map tool_result to CodeWhisperer's toolResults format.
	// Instead, tool results are included as text content via getMessageContent,
	// which the model can understand from context. This avoids format mismatches
	// with CodeWhisperer's undocumented schema.

	// Build history from caller-provided conversation only.
	history := make([]any, 0, len(anthropicReq.Messages)-1)
	for i := 0; i < len(anthropicReq.Messages)-1; i++ {
		msg := anthropicReq.Messages[i]
		content := getMessageContent(msg.Content)
		if strings.TrimSpace(content) == "" {
			continue
		}

		if msg.Role == "assistant" {
			assistantMsg := HistoryAssistantMessage{}
			assistantMsg.AssistantResponseMessage.Content = content
			assistantMsg.AssistantResponseMessage.ToolUses = make([]any, 0)
			history = append(history, assistantMsg)
			continue
		}

		userMsg := HistoryUserMessage{}
		userMsg.UserInputMessage.Content = content
		userMsg.UserInputMessage.ModelId = resolvedModel
		userMsg.UserInputMessage.Origin = "AI_EDITOR"
		history = append(history, userMsg)
	}
	cwReq.ConversationState.History = history

	return cwReq
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  kirolink read    - Read and display token")
		fmt.Println("  kirolink refresh - Refresh token")
		fmt.Println("  kirolink export  - Export environment variables")
		fmt.Println("  kirolink claude  - Skip Claude region restrictions")
		fmt.Println("  kirolink server [port] - Start Anthropic API proxy server")
		fmt.Println("  author https://github.com/alexandeism/kirolink")
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

func legacyClaudeConfigKey() string {
	return strings.Join([]string{"kiro", "2cc"}, "")
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
	jsonData["kirolink"] = true
	delete(jsonData, legacyClaudeConfigKey())

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
		resolvedModel := resolveModelID(anthropicReq.Model)
		if strings.TrimSpace(anthropicReq.Model) == "" {
			anthropicReq.Model = "default"
		}
		if _, ok := ModelMap[strings.ToLower(strings.TrimSpace(anthropicReq.Model))]; !ok {
			fmt.Printf("Warning: Unknown model alias %q, using fallback %q\n", anthropicReq.Model, resolvedModel)
		}

		// Handle streaming request
		if anthropicReq.Stream {
			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("PANIC in streaming handler: %v\n", r)
					}
				}()
				handleStreamRequest(w, anthropicReq, token.AccessToken)
			}()
			return
		}

		// Handle non-streaming request
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("PANIC in non-streaming handler: %v\n", r)
					http.Error(w, fmt.Sprintf(`{"error":{"type":"server_error","message":"Internal panic: %v"}}`, r), http.StatusInternalServerError)
				}
			}()
			handleNonStreamRequest(w, anthropicReq, token.AccessToken)
		}()
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

type anthropicResponseBlock struct {
	Type      string
	Text      string
	ToolUseID string
	ToolName  string
	ToolInput map[string]any
	rawInput  string
}

type translatedAnthropicResponse struct {
	Blocks       []anthropicResponseBlock
	StopReason   string
	OutputTokens int
}

func responseModelID(cwReq CodeWhispererRequest, anthropicReq AnthropicRequest) string {
	modelID := strings.TrimSpace(cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId)
	if modelID != "" {
		return modelID
	}
	return anthropicReq.Model
}

func assembleAnthropicResponse(events []protocol.SSEEvent) translatedAnthropicResponse {
	type blockAccumulator struct {
		anthropicResponseBlock
	}

	blocks := map[int]*blockAccumulator{}
	order := []int{}
	stopReason := ""
	outputTokens := 0

	ensureBlock := func(index int, blockType string) *blockAccumulator {
		if block, ok := blocks[index]; ok {
			if blockType != "" && block.Type == "" {
				block.Type = blockType
			}
			return block
		}

		block := &blockAccumulator{}
		block.Type = blockType
		blocks[index] = block
		order = append(order, index)
		return block
	}

	for _, event := range events {
		dataMap, ok := event.Data.(map[string]any)
		if !ok {
			continue
		}

		switch dataMap["type"] {
		case "content_block_start":
			index := eventIndex(dataMap["index"])
			contentBlock, _ := dataMap["content_block"].(map[string]any)
			blockType, _ := contentBlock["type"].(string)
			block := ensureBlock(index, blockType)
			if blockType == "tool_use" {
				block.ToolUseID, _ = contentBlock["id"].(string)
				block.ToolName, _ = contentBlock["name"].(string)
				if input, ok := contentBlock["input"].(map[string]any); ok {
					block.ToolInput = input
				}
			}
		case "content_block_delta":
			index := eventIndex(dataMap["index"])
			deltaMap, _ := dataMap["delta"].(map[string]any)
			deltaType, _ := deltaMap["type"].(string)
			blockType := ""
			if deltaType == "text_delta" {
				blockType = "text"
			} else if deltaType == "input_json_delta" {
				blockType = "tool_use"
			}
			block := ensureBlock(index, blockType)

			switch deltaType {
			case "text_delta":
				text, _ := deltaMap["text"].(string)
				block.Text += text
				outputTokens++
			case "input_json_delta":
				if block.ToolUseID == "" {
					block.ToolUseID, _ = deltaMap["id"].(string)
				}
				if block.ToolName == "" {
					block.ToolName, _ = deltaMap["name"].(string)
				}
				switch partial := deltaMap["partial_json"].(type) {
				case string:
					block.rawInput += partial
				case *string:
					if partial != nil {
						block.rawInput += *partial
					}
				}
				outputTokens++
			}
		case "message_delta":
			if deltaMap, ok := dataMap["delta"].(map[string]any); ok {
				if reason, _ := deltaMap["stop_reason"].(string); reason != "" {
					stopReason = reason
				}
			}
		}
	}

	sort.Ints(order)
	translated := translatedAnthropicResponse{StopReason: stopReason, OutputTokens: outputTokens}
	translated.Blocks = make([]anthropicResponseBlock, 0, len(order))

	for _, index := range order {
		block := blocks[index]
		if block == nil || block.Type == "" {
			continue
		}

		if block.Type == "tool_use" {
			if strings.TrimSpace(block.rawInput) != "" {
				toolInput := map[string]any{}
				if err := jsonStr.Unmarshal([]byte(block.rawInput), &toolInput); err != nil {
					log.Printf("tool input unmarshal error: %v", err)
				} else {
					block.ToolInput = toolInput
				}
			}
			if block.ToolInput == nil {
				block.ToolInput = map[string]any{}
			}
		}

		translated.Blocks = append(translated.Blocks, block.anthropicResponseBlock)
	}

	if translated.StopReason == "" {
		translated.StopReason = "end_turn"
		if len(translated.Blocks) > 0 && translated.Blocks[len(translated.Blocks)-1].Type == "tool_use" {
			translated.StopReason = "tool_use"
		}
	}

	return translated
}

func eventIndex(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func buildAnthropicResponsePayload(model string, inputTokens int, translated translatedAnthropicResponse) map[string]any {
	content := make([]map[string]any, 0, len(translated.Blocks))
	for _, block := range translated.Blocks {
		switch block.Type {
		case "text":
			content = append(content, map[string]any{
				"type": "text",
				"text": block.Text,
			})
		case "tool_use":
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    block.ToolUseID,
				"name":  block.ToolName,
				"input": block.ToolInput,
			})
		}
	}

	return map[string]any{
		"content":       content,
		"model":         model,
		"role":          "assistant",
		"stop_reason":   translated.StopReason,
		"stop_sequence": nil,
		"type":          "message",
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": translated.OutputTokens,
		},
	}
}

func buildAnthropicStreamEvents(messageId, model string, inputTokens int, translated translatedAnthropicResponse) []protocol.SSEEvent {
	events := []protocol.SSEEvent{{
		Event: "message_start",
		Data: map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            messageId,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  inputTokens,
					"output_tokens": 1,
				},
			},
		},
	}, {
		Event: "ping",
		Data:  map[string]string{"type": "ping"},
	}}

	for index, block := range translated.Blocks {
		switch block.Type {
		case "text":
			events = append(events,
				protocol.SSEEvent{
					Event: "content_block_start",
					Data: map[string]any{
						"type":  "content_block_start",
						"index": index,
						"content_block": map[string]any{
							"type": "text",
							"text": "",
						},
					},
				},
				protocol.SSEEvent{
					Event: "content_block_delta",
					Data: map[string]any{
						"type":  "content_block_delta",
						"index": index,
						"delta": map[string]any{
							"type": "text_delta",
							"text": block.Text,
						},
					},
				},
				protocol.SSEEvent{
					Event: "content_block_stop",
					Data: map[string]any{
						"type":  "content_block_stop",
						"index": index,
					},
				},
			)
		case "tool_use":
			events = append(events, protocol.SSEEvent{
				Event: "content_block_start",
				Data: map[string]any{
					"type":  "content_block_start",
					"index": index,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    block.ToolUseID,
						"name":  block.ToolName,
						"input": map[string]any{},
					},
				},
			})

			if len(block.ToolInput) > 0 {
				if rawInput, err := jsonStr.Marshal(block.ToolInput); err == nil {
					events = append(events, protocol.SSEEvent{
						Event: "content_block_delta",
						Data: map[string]any{
							"type":  "content_block_delta",
							"index": index,
							"delta": map[string]any{
								"type":         "input_json_delta",
								"id":           block.ToolUseID,
								"name":         block.ToolName,
								"partial_json": string(rawInput),
							},
						},
					})
				}
			}

			events = append(events, protocol.SSEEvent{
				Event: "content_block_stop",
				Data: map[string]any{
					"type":  "content_block_stop",
					"index": index,
				},
			})
		}
	}

	events = append(events,
		protocol.SSEEvent{
			Event: "message_delta",
			Data: map[string]any{
				"type": "message_delta",
				"delta": map[string]any{
					"stop_reason":   translated.StopReason,
					"stop_sequence": nil,
				},
				"usage": map[string]any{"output_tokens": translated.OutputTokens},
			},
		},
		protocol.SSEEvent{
			Event: "message_stop",
			Data:  map[string]any{"type": "message_stop"},
		},
	)

	return events
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

	// Serialize with payload-size enforcement
	cwReqBody, err := ensurePayloadFits(&cwReq)
	if err != nil {
		sendErrorEvent(w, flusher, "Failed to serialize request", err)
		return
	}

	fmt.Printf("CodeWhisperer streaming request body:\n%s\n", string(cwReqBody))

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

	// Send request with retry on "Improperly formed request"
	client := &http.Client{}

	var resp *http.Response
	const maxRetries = 2
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Rebuild the HTTP request with the (possibly trimmed) body
			proxyReq, err = http.NewRequest(
				http.MethodPost,
				"https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse",
				bytes.NewBuffer(cwReqBody),
			)
			if err != nil {
				sendErrorEvent(w, flusher, "Failed to create retry request", err)
				return
			}
			proxyReq.Header.Set("Authorization", "Bearer "+accessToken)
			proxyReq.Header.Set("Content-Type", "application/json")
			proxyReq.Header.Set("Accept", "text/event-stream")
		}

		resp, err = client.Do(proxyReq)
		if err != nil {
			sendErrorEvent(w, flusher, "CodeWhisperer request error", fmt.Errorf("request error: %s", err.Error()))
			return
		}

		if resp.StatusCode == http.StatusOK {
			break // success
		}

		respBodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		respStr := string(respBodyBytes)
		fmt.Printf("CodeWhisperer STREAM response error, status code: %d, response: %s\n", resp.StatusCode, respStr)

		if resp.StatusCode == 400 && strings.Contains(respStr, "Improperly formed request") && attempt < maxRetries-1 {
			fmt.Printf("CodeWhisperer STREAM improperly formed request; retrying with trimmed payload (attempt %d)\n", attempt+1)
			// Aggressively trim retry payload while keeping the most recent caller history.
			cwReq.ConversationState.History = keepMostRecentHistory(cwReq.ConversationState.History, 2)
			// Strip tools to just name + minimal schema
			tools := cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools
			for i := range tools {
				tools[i].ToolSpecification.Description = truncateString(tools[i].ToolSpecification.Description, 80)
				tools[i].ToolSpecification.InputSchema = InputSchema{Json: map[string]any{"type": "object"}}
			}
			cwReqBody, err = jsonStr.Marshal(cwReq)
			if err != nil {
				sendErrorEvent(w, flusher, "Failed to serialize retry request", err)
				return
			}
			fmt.Printf("[retry] trimmed payload size: %d bytes\n", len(cwReqBody))
			continue
		}

		// Non-retryable error
		if resp.StatusCode == 403 {
			refreshToken()
			sendErrorEvent(w, flusher, "error", fmt.Errorf("CodeWhisperer Token refreshed, please retry"))
		} else {
			sendErrorEvent(w, flusher, "error", fmt.Errorf("CodeWhisperer Error: %s", respStr))
		}
		return
	}
	defer resp.Body.Close()

	// Read full response body first
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		sendErrorEvent(w, flusher, "error", fmt.Errorf("CodeWhisperer Error: Failed to read response"))
		return
	}

	// os.WriteFile(messageId+"response.raw", respBody, 0644)

	parsedEvents := protocol.ParseEvents(respBody)

	if len(parsedEvents) > 0 {
		translated := assembleAnthropicResponse(parsedEvents)
		streamEvents := buildAnthropicStreamEvents(
			messageId,
			responseModelID(cwReq, anthropicReq),
			len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Content),
			translated,
		)
		for _, event := range streamEvents {
			sendSSEEvent(w, flusher, event.Event, event.Data)
		}
	}

}

// handleNonStreamRequest handles non-streaming requests
func handleNonStreamRequest(w http.ResponseWriter, anthropicReq AnthropicRequest, accessToken string) {
	// Build CodeWhisperer request
	cwReq := buildCodeWhispererRequest(anthropicReq)

	// Serialize with payload-size enforcement
	cwReqBody, err := ensurePayloadFits(&cwReq)
	if err != nil {
		fmt.Printf("Error: Failed to serialize request: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to serialize request: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Printf("CodeWhisperer request body:\n%s\n", string(cwReqBody))

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

	fmt.Printf("CodeWhisperer response body:\n%s\n", string(cwRespBody))

	respBodyStr := string(cwRespBody)

	translated := assembleAnthropicResponse(protocol.ParseEvents(cwRespBody))

	// Check if response is an error
	if strings.Contains(string(cwRespBody), "Improperly formed request.") {
		fmt.Printf("Error: CodeWhisperer returned incorrect format: %s\n", respBodyStr)
		http.Error(w, fmt.Sprintf("Request format error: %s", respBodyStr), http.StatusBadRequest)
		return
	}

	// Build Anthropic response
	anthropicResp := buildAnthropicResponsePayload(
		responseModelID(cwReq, anthropicReq),
		len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Content),
		translated,
	)

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
