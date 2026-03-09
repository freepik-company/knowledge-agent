package slack

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"knowledge-agent/internal/logger"
)

const (
	maxMessageChars = 500
	maxContextChars = 4000
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
// Includes thread context from previous messages when available.
func buildSlackUserMessage(query, userName, userRealName string, messageData []map[string]any) string {
	var sb strings.Builder

	// Add thread context from previous messages
	threadCtx := formatThreadContext(messageData)
	if threadCtx != "" {
		sb.WriteString(threadCtx)
		sb.WriteString("\n")
	}

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

// formatThreadContext formats previous thread messages for LLM context.
// Excludes the last message (the current user query). Returns empty string
// if there are no previous messages.
func formatThreadContext(messageData []map[string]any) string {
	if len(messageData) <= 1 {
		return ""
	}

	// Exclude the last message (current user query)
	previous := messageData[:len(messageData)-1]

	var sb strings.Builder
	sb.WriteString("--- Thread Context ---\n")

	totalChars := 0
	// Build from most recent to oldest, then reverse to keep chronological order
	var lines []string
	for i := len(previous) - 1; i >= 0; i-- {
		msg := previous[i]

		user := getStringFromMap(msg, "user_name")
		if user == "" {
			user = getStringFromMap(msg, "user")
		}
		if user == "" {
			user = "Unknown"
		}

		text := getStringFromMap(msg, "text")
		if runes := []rune(text); len(runes) > maxMessageChars {
			text = string(runes[:maxMessageChars]) + "..."
		}

		var line strings.Builder
		fmt.Fprintf(&line, "[%d] %s: %s\n", i+1, user, text)

		if ts := getStringFromMap(msg, "ts"); ts != "" {
			fmt.Fprintf(&line, "   (time: %s)\n", formatSlackTimestamp(ts))
		}

		if images, ok := msg["images"].([]any); ok && len(images) > 0 {
			fmt.Fprintf(&line, "   [%d image(s) attached]\n", len(images))
		}

		lineStr := line.String()
		if totalChars+len(lineStr) > maxContextChars {
			break
		}
		totalChars += len(lineStr)
		lines = append(lines, lineStr)
	}

	// Reverse to chronological order
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	for _, l := range lines {
		sb.WriteString(l)
	}

	sb.WriteString("--- End Thread Context ---")
	return sb.String()
}

// getStringFromMap safely extracts a string value from a map.
func getStringFromMap(m map[string]any, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// formatSlackTimestamp converts a Slack timestamp (e.g. "1234567890.123456")
// to a human-readable format like "2006-01-02 15:04:05 UTC".
func formatSlackTimestamp(ts string) string {
	if ts == "" {
		return ""
	}
	parts := strings.Split(ts, ".")
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ts
	}
	return time.Unix(seconds, 0).UTC().Format("2006-01-02 15:04:05 UTC")
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
