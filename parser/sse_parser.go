package parser

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"strings"
)

type assistantResponseEvent struct {
	Content   string  `json:"content"`
	Input     *string `json:"input,omitempty"`
	Name      string  `json:"name"`
	ToolUseId string  `json:"toolUseId"`
	Stop      bool    `json:"stop"`
}

type SSEEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

func ParseEvents(resp []byte) []SSEEvent {

	events := []SSEEvent{}

	r := bytes.NewReader(resp)
	for {
		if r.Len() < 12 {
			break
		}

		var totalLen, headerLen uint32
		if err := binary.Read(r, binary.BigEndian, &totalLen); err != nil {
			break
		}
		if err := binary.Read(r, binary.BigEndian, &headerLen); err != nil {
			break
		}

		if int(totalLen) > r.Len()+8 {
			log.Println("Frame length invalid")
			break
		}

		// Skip header
		header := make([]byte, headerLen)
		if _, err := io.ReadFull(r, header); err != nil {
			break
		}

		payloadLen := int(totalLen) - int(headerLen) - 12
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}

		// Skip CRC32
		if _, err := r.Seek(4, io.SeekCurrent); err != nil {
			break
		}

		// Handle binary framing and clean up payload
		payloadStr := string(payload)
		if idx := strings.Index(payloadStr, "{"); idx != -1 {
			payloadStr = payloadStr[idx:]
		}

		// First try parsing as assistantResponseEvent
		var assistantEvt assistantResponseEvent
		if err := json.Unmarshal([]byte(payloadStr), &assistantEvt); err == nil && (assistantEvt.Content != "" || assistantEvt.ToolUseId != "" || assistantEvt.Stop) {
			events = append(events, convertAssistantEventToSSE(assistantEvt))

			if assistantEvt.ToolUseId != "" && assistantEvt.Name != "" && assistantEvt.Stop {
				events = append(events, SSEEvent{
					Event: "message_delta",
					Data: map[string]interface{}{
						"type": "message_delta",
						"delta": map[string]interface{}{
							"stop_reason":   "tool_use",
							"stop_sequence": nil,
						},
						"usage": map[string]interface{}{"output_tokens": 0},
					},
				})
			}
			continue
		}

		// Handling 2026+ metadata events (metering, context usage)
		var metaData map[string]any
		if err := json.Unmarshal([]byte(payloadStr), &metaData); err == nil {
			if _, exists := metaData["contextUsagePercentage"]; exists {
				// Translate to a ping/metadata event for Claude Code
				events = append(events, SSEEvent{
					Event: "ping",
					Data:  map[string]any{"type": "ping", "metadata": metaData},
				})
			} else if _, exists := metaData["unit"]; exists {
				// Metering event - silently consume or log lightly
				log.Printf("Usage: %v %v", metaData["usage"], metaData["unit"])
			}
		}
	}

	return events
}

func convertAssistantEventToSSE(evt assistantResponseEvent) SSEEvent {
	if evt.Content != "" {
		return SSEEvent{
			Event: "content_block_delta",
			Data: map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": evt.Content,
				},
			},
		}
	} else if evt.ToolUseId != "" && evt.Name != "" && !evt.Stop {

		if evt.Input == nil {
			return SSEEvent{
				Event: "content_block_start",
				Data: map[string]interface{}{
					"type":  "content_block_start",
					"index": 1,
					"content_block": map[string]interface{}{
						"type":  "tool_use",
						"id":    evt.ToolUseId,
						"name":  evt.Name,
						"input": map[string]interface{}{},
					},
				},
			}
		} else {
			return SSEEvent{
				Event: "content_block_delta",
				Data: map[string]interface{}{
					"type":  "content_block_delta",
					"index": 1,
					"delta": map[string]interface{}{
						"type":         "input_json_delta",
						"id":           evt.ToolUseId,
						"name":         evt.Name,
						"partial_json": evt.Input,
					},
				},
			}
		}

	} else if evt.Stop {
		return SSEEvent{
			Event: "content_block_stop",
			Data: map[string]interface{}{
				"type":  "content_block_stop",
				"index": 1,
			},
		}
	}

	return SSEEvent{}
}