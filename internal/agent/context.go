package agent

import (
	"fmt"
	"strings"
)

// buildThreadContext builds a formatted conversation from messages for ingestion
func (a *Agent) buildThreadContext(req IngestRequest) string {
	var builder strings.Builder

	for i, msg := range req.Messages {
		// Prefer user_name (resolved display name) over raw user ID
		user := getStringFromMap(msg, "user_name")
		if user == "" {
			user = getStringFromMap(msg, "user")
		}
		text := getStringFromMap(msg, "text")
		ts := getStringFromMap(msg, "ts")

		if user == "" {
			user = "Unknown"
		}

		// Format: [timestamp] User: message
		fmt.Fprintf(&builder, "[%d] %s: %s\n", i+1, user, text)

		// Add metadata if available
		if ts != "" {
			fmt.Fprintf(&builder, "   (timestamp: %s)\n", ts)
		}
	}

	return builder.String()
}

// buildThreadContextFromMessages builds context from a slice of message maps for queries
func (a *Agent) buildThreadContextFromMessages(messages []map[string]any) string {
	var builder strings.Builder

	for i, msg := range messages {
		// Prefer user_name (resolved display name) over raw user ID
		user := getStringFromMap(msg, "user_name")
		if user == "" {
			user = getStringFromMap(msg, "user")
		}
		text := getStringFromMap(msg, "text")
		ts := getStringFromMap(msg, "ts")

		if user == "" {
			user = "Unknown"
		}

		// Format: [timestamp] User: message
		fmt.Fprintf(&builder, "[%d] %s: %s\n", i+1, user, text)

		// Add metadata if available
		if ts != "" {
			fmt.Fprintf(&builder, "   (timestamp: %s)\n", ts)
		}

		// Add image references if present
		if images, ok := msg["images"].([]any); ok && len(images) > 0 {
			fmt.Fprintf(&builder, "   📷 Attached %d image(s)\n", len(images))
		}
	}

	return builder.String()
}

// getStringFromMap safely extracts a string value from a map
func getStringFromMap(m map[string]any, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
