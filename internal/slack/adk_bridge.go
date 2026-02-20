package slack

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"knowledge-agent/internal/logger"
)

// resolveSlackSessionID determines the session ID for Slack context.
// Thread context → channel fallback → API-only.
func resolveSlackSessionID(channelID, threadTS string) string {
	if channelID != "" && threadTS != "" {
		return fmt.Sprintf("thread-%s-%s", channelID, threadTS)
	}
	if channelID != "" {
		return fmt.Sprintf("channel-%s", channelID)
	}
	return "api-slack"
}

// resolveSlackUserID determines the user ID based on knowledge scope.
func resolveSlackUserID(scope, channelID, slackUserID string) string {
	switch scope {
	case "shared":
		return "shared-knowledge"
	case "channel":
		if channelID != "" {
			return channelID
		}
		return "shared-knowledge"
	case "user":
		if slackUserID != "" {
			return slackUserID
		}
		return "shared-knowledge"
	default:
		return "shared-knowledge"
	}
}

// buildSlackUserMessage builds the user message text with Slack context.
func buildSlackUserMessage(query, userName, userRealName string, messageData []map[string]any) string {
	var sb strings.Builder

	// Add user context
	if userRealName != "" {
		fmt.Fprintf(&sb, "**User**: %s (@%s)\n", userRealName, userName)
	} else if userName != "" {
		fmt.Fprintf(&sb, "**User**: @%s\n", userName)
	}

	// Add the query
	sb.WriteString(query)

	return sb.String()
}

// buildADKContent creates a genai.Content-compatible map for the ADK request.
// Includes text and optional images from the last message.
func buildADKContent(text string, messageData []map[string]any) map[string]any {
	parts := []map[string]any{
		{"text": text},
	}

	// Add images from the last message (if present)
	if len(messageData) > 0 {
		lastMsg := messageData[len(messageData)-1]
		if images, ok := lastMsg["images"].([]any); ok {
			for _, imgRaw := range images {
				img, ok := imgRaw.(map[string]any)
				if !ok {
					continue
				}
				mimeType, _ := img["mime_type"].(string)
				base64Data, _ := img["data"].(string)

				if mimeType != "" && base64Data != "" {
					// Validate base64 encoding
					if _, err := base64.StdEncoding.DecodeString(base64Data); err != nil {
						continue
					}
					parts = append(parts, map[string]any{
						"inlineData": map[string]any{
							"mimeType": mimeType,
							"data":     base64Data,
						},
					})
				}
			}
		}
	}

	return map[string]any{
		"role":  "user",
		"parts": parts,
	}
}

// adkEvent represents a single event in the ADK /agent/run response.
type adkEvent struct {
	Content      *adkContent `json:"content,omitempty"`
	TurnComplete bool        `json:"turnComplete,omitempty"`
	ErrorCode    string      `json:"errorCode,omitempty"`
	ErrorMessage string      `json:"errorMessage,omitempty"`
}

type adkContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []adkPart `json:"parts,omitempty"`
}

type adkPart struct {
	Text             string `json:"text,omitempty"`
	FunctionCall     any    `json:"functionCall,omitempty"`
	FunctionResponse any    `json:"functionResponse,omitempty"`
}

// extractAnswerFromADKResponse parses the ADK /agent/run response (JSON array of events)
// and extracts the final text answer, filtering out intermediate tool-call "thinking" text.
func extractAnswerFromADKResponse(body io.Reader) string {
	log := logger.Get()

	var events []adkEvent
	if err := json.NewDecoder(body).Decode(&events); err != nil {
		log.Errorw("Failed to decode ADK response", "error", err)
		return ""
	}

	// Collect text from model events, filtering out "thinking" text that accompanies tool calls
	var answerParts []string

	for _, event := range events {
		if event.ErrorCode != "" {
			log.Warnw("ADK event error",
				"error_code", event.ErrorCode,
				"error_message", event.ErrorMessage,
			)
			continue
		}

		if event.Content == nil || event.Content.Role != "model" {
			continue
		}

		// Check if this event has any tool calls
		hasToolCall := false
		for _, part := range event.Content.Parts {
			if part.FunctionCall != nil {
				hasToolCall = true
				break
			}
		}

		// Only collect text from events without tool calls (final response text)
		if !hasToolCall {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					answerParts = append(answerParts, part.Text)
				}
			}
		}
	}

	return strings.Join(answerParts, "")
}
