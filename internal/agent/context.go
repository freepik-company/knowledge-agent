package agent

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// buildThreadContextFromMessages builds context from a slice of message maps for queries and ingestion
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

		// Format: [index] User: message
		fmt.Fprintf(&builder, "[%d] %s: %s\n", i+1, user, text)

		// Add human-readable timestamp if available
		if ts != "" {
			humanTime := formatSlackTimestamp(ts)
			fmt.Fprintf(&builder, "   (time: %s)\n", humanTime)
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

// formatSlackTimestamp converts a Slack timestamp (Unix epoch with microseconds)
// to a human-readable format that the LLM can understand
// Input: "1769678919.472419" -> Output: "2026-01-29 09:48:39 UTC"
func formatSlackTimestamp(ts string) string {
	if ts == "" {
		return ""
	}

	// Slack timestamp format: "1234567890.123456" (seconds.microseconds)
	// We only need the seconds part
	parts := strings.Split(ts, ".")
	if len(parts) == 0 {
		return ts // Return original if can't parse
	}

	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ts // Return original if can't parse
	}

	t := time.Unix(seconds, 0).UTC()
	return t.Format("2006-01-02 15:04:05 UTC")
}
