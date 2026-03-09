package slack

import (
	"strings"
	"testing"
)

func TestGetStringFromMap(t *testing.T) {
	m := map[string]any{
		"name": "alice",
		"count": 42,
	}

	if got := getStringFromMap(m, "name"); got != "alice" {
		t.Errorf("expected 'alice', got %q", got)
	}
	if got := getStringFromMap(m, "missing"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	if got := getStringFromMap(m, "count"); got != "" {
		t.Errorf("expected empty for non-string, got %q", got)
	}
}

func TestFormatSlackTimestamp(t *testing.T) {
	tests := []struct {
		name string
		ts   string
		want string
	}{
		{"empty", "", ""},
		{"valid", "1769678919.472419", "2026-01-29 09:28:39 UTC"},
		{"invalid", "not-a-number", "not-a-number"},
		{"no dot", "1769678919", "2026-01-29 09:28:39 UTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatSlackTimestamp(tt.ts); got != tt.want {
				t.Errorf("formatSlackTimestamp(%q) = %q, want %q", tt.ts, got, tt.want)
			}
		})
	}
}

func TestFormatThreadContext_Empty(t *testing.T) {
	if got := formatThreadContext(nil); got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
	if got := formatThreadContext([]map[string]any{}); got != "" {
		t.Errorf("expected empty for empty slice, got %q", got)
	}
}

func TestFormatThreadContext_SingleMessage(t *testing.T) {
	msgs := []map[string]any{
		{"user": "U123", "text": "hello"},
	}
	if got := formatThreadContext(msgs); got != "" {
		t.Errorf("expected empty for single message (current query), got %q", got)
	}
}

func TestFormatThreadContext_MultipleMessages(t *testing.T) {
	msgs := []map[string]any{
		{"user": "U001", "text": "first message", "ts": "1769678900.000000"},
		{"user_name": "bob", "text": "second message", "ts": "1769678910.000000"},
		{"user": "U003", "text": "current query"},
	}
	got := formatThreadContext(msgs)

	if !strings.Contains(got, "--- Thread Context ---") {
		t.Error("missing opening delimiter")
	}
	if !strings.Contains(got, "--- End Thread Context ---") {
		t.Error("missing closing delimiter")
	}
	if !strings.Contains(got, "[1] U001: first message") {
		t.Error("missing first message")
	}
	if !strings.Contains(got, "[2] bob: second message") {
		t.Error("missing second message with user_name")
	}
	if strings.Contains(got, "current query") {
		t.Error("should not contain the last message (current query)")
	}
}

func TestFormatThreadContext_WithImages(t *testing.T) {
	msgs := []map[string]any{
		{"user": "U001", "text": "look at this", "images": []any{map[string]any{"data": "abc"}}},
		{"user": "U002", "text": "current"},
	}
	got := formatThreadContext(msgs)

	if !strings.Contains(got, "[1 image(s) attached]") {
		t.Error("missing image annotation")
	}
}

func TestFormatThreadContext_BotMessages(t *testing.T) {
	msgs := []map[string]any{
		{"user": "UBOT", "user_name": "knowledge-bot", "text": "I found the answer"},
		{"user": "U001", "text": "thanks"},
	}
	got := formatThreadContext(msgs)

	if !strings.Contains(got, "knowledge-bot: I found the answer") {
		t.Error("bot message should be included with user_name")
	}
}

func TestFormatThreadContext_TruncatesLongMessages(t *testing.T) {
	longText := strings.Repeat("x", 600)
	msgs := []map[string]any{
		{"user": "U001", "text": longText},
		{"user": "U002", "text": "current"},
	}
	got := formatThreadContext(msgs)

	// The message should be truncated to maxMessageChars + "..."
	if strings.Contains(got, longText) {
		t.Error("long message should be truncated")
	}
	if !strings.Contains(got, "...") {
		t.Error("truncated message should end with ellipsis")
	}
}

func TestFormatThreadContext_CapsTotal(t *testing.T) {
	// Create many messages that exceed maxContextChars
	var msgs []map[string]any
	for i := 0; i < 50; i++ {
		msgs = append(msgs, map[string]any{
			"user": "U001",
			"text": strings.Repeat("a", 200),
		})
	}
	// Add current query
	msgs = append(msgs, map[string]any{"user": "U002", "text": "current"})

	got := formatThreadContext(msgs)

	// Should be capped and keep most recent messages
	if len(got) > maxContextChars+200 { // some slack for delimiters
		t.Errorf("context too long: %d chars", len(got))
	}
	// Most recent messages should be present (higher indices)
	if !strings.Contains(got, "[50]") {
		t.Error("should contain the most recent previous message")
	}
}

func TestBuildSlackUserMessage_WithThreadContext(t *testing.T) {
	msgs := []map[string]any{
		{"user": "U001", "text": "what is the deploy process?"},
		{"user_name": "bot", "text": "Here is the process..."},
		{"user": "U003", "text": "thanks, and how about rollback?"},
	}
	got := buildSlackUserMessage("thanks, and how about rollback?", "alice", "Alice Smith", msgs)

	if !strings.Contains(got, "--- Thread Context ---") {
		t.Error("missing thread context")
	}
	if !strings.Contains(got, "**User**: Alice Smith (@alice)") {
		t.Error("missing user context")
	}
	if !strings.HasSuffix(got, "thanks, and how about rollback?") {
		t.Error("should end with query")
	}
}

func TestBuildSlackUserMessage_WithoutThreadContext(t *testing.T) {
	msgs := []map[string]any{
		{"user": "U001", "text": "hello"},
	}
	got := buildSlackUserMessage("hello", "alice", "", msgs)

	if strings.Contains(got, "Thread Context") {
		t.Error("should not have thread context for single message")
	}
	if !strings.Contains(got, "**User**: @alice") {
		t.Error("missing user context")
	}
}

func TestBuildSlackUserMessage_NilMessages(t *testing.T) {
	got := buildSlackUserMessage("hello", "alice", "Alice", nil)

	if strings.Contains(got, "Thread Context") {
		t.Error("should not have thread context for nil messages")
	}
	if !strings.Contains(got, "hello") {
		t.Error("should contain query")
	}
}
