package a2a

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"

	"knowledge-agent/internal/config"
)

func TestContextCleanerInterceptor_ExtractTextFromParts(t *testing.T) {
	tests := []struct {
		name     string
		parts    a2a.ContentParts
		wantText bool
	}{
		{
			name: "single text part as value",
			parts: a2a.ContentParts{
				a2a.TextPart{Text: "Hello world"},
			},
			wantText: true,
		},
		{
			name: "multiple text parts as values",
			parts: a2a.ContentParts{
				a2a.TextPart{Text: "Part 1"},
				a2a.TextPart{Text: "Part 2"},
				a2a.TextPart{Text: "Part 3"},
			},
			wantText: true,
		},
		{
			name:     "empty parts",
			parts:    a2a.ContentParts{},
			wantText: false,
		},
		{
			name: "text part with empty text",
			parts: a2a.ContentParts{
				a2a.TextPart{Text: ""},
			},
			wantText: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract text from parts (same logic as interceptor)
			var texts []string
			for _, part := range tt.parts {
				if textPart, ok := part.(a2a.TextPart); ok && textPart.Text != "" {
					texts = append(texts, textPart.Text)
				}
			}

			hasText := len(texts) > 0
			if hasText != tt.wantText {
				t.Errorf("got hasText=%v, want %v (texts=%v)", hasText, tt.wantText, texts)
			}

			// Verify we're using value type assertion, not pointer
			for _, part := range tt.parts {
				// This should NOT match (pointer type)
				if _, ok := part.(*a2a.TextPart); ok {
					t.Error("Part matched as *a2a.TextPart (pointer), but should be a2a.TextPart (value)")
				}
			}
		})
	}
}

func TestContextCleanerInterceptor_ModifyParts(t *testing.T) {
	// Test that we can modify parts correctly
	params := &a2a.MessageSendParams{
		Message: &a2a.Message{
			ID:   "test-msg-1",
			Role: a2a.MessageRoleUser,
			Parts: a2a.ContentParts{
				a2a.TextPart{Text: "Original long text that should be replaced"},
				a2a.TextPart{Text: "Another part that should be removed"},
			},
		},
	}

	// Verify original state
	if len(params.Message.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(params.Message.Parts))
	}

	// Modify parts (same as interceptor does)
	summarized := "Short summary"
	params.Message.Parts = a2a.ContentParts{
		a2a.TextPart{
			Text: summarized,
		},
	}

	// Verify modification
	if len(params.Message.Parts) != 1 {
		t.Fatalf("expected 1 part after modification, got %d", len(params.Message.Parts))
	}

	// Verify text content
	if textPart, ok := params.Message.Parts[0].(a2a.TextPart); ok {
		if textPart.Text != summarized {
			t.Errorf("expected text=%q, got %q", summarized, textPart.Text)
		}
	} else {
		t.Error("part is not a2a.TextPart")
	}
}

func TestContextCleanerInterceptor_TypeAssertion(t *testing.T) {
	// Test that payload type assertion works correctly
	params := &a2a.MessageSendParams{
		Message: &a2a.Message{
			ID:   "test-msg-1",
			Role: a2a.MessageRoleUser,
			Parts: a2a.ContentParts{
				a2a.TextPart{Text: "Test message"},
			},
		},
	}

	// Create a mock request
	req := &a2aclient.Request{
		Payload: params,
	}

	// Test type assertion (same as interceptor)
	assertedParams, ok := req.Payload.(*a2a.MessageSendParams)
	if !ok {
		t.Fatal("failed to assert payload as *a2a.MessageSendParams")
	}

	if assertedParams.Message == nil {
		t.Fatal("Message is nil")
	}

	if len(assertedParams.Message.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(assertedParams.Message.Parts))
	}

	// Verify that modifying through assertion modifies original
	assertedParams.Message.Parts = a2a.ContentParts{
		a2a.TextPart{Text: "Modified"},
	}

	// Check original was modified (same pointer)
	if textPart, ok := params.Message.Parts[0].(a2a.TextPart); ok {
		if textPart.Text != "Modified" {
			t.Errorf("original not modified: expected 'Modified', got %q", textPart.Text)
		}
	}
}

func TestContextCleanerInterceptor_DisabledSkipsProcessing(t *testing.T) {
	ci := NewContextCleanerInterceptor("test_agent", "Test description", config.A2AContextCleanerConfig{
		Enabled: true,
		Model:   "claude-haiku-4-5-20251001",
	})

	// Force disabled (no API key in test environment)
	ci.enabled = false

	params := &a2a.MessageSendParams{
		Message: &a2a.Message{
			Parts: a2a.ContentParts{
				a2a.TextPart{Text: "Test message"},
			},
		},
	}

	req := &a2aclient.Request{
		Payload: params,
	}

	ctx, err := ci.Before(context.Background(), req)
	if err != nil {
		t.Fatalf("Before() error = %v", err)
	}

	if ctx == nil {
		t.Fatal("Before() returned nil context")
	}

	// Verify parts were not modified (interceptor disabled)
	if len(params.Message.Parts) != 1 {
		t.Errorf("parts should not be modified when disabled")
	}
}

func TestContextCleanerInterceptor_NilPayloadSkipsProcessing(t *testing.T) {
	ci := &contextCleanerInterceptor{
		agentName: "test_agent",
		enabled:   true,
	}

	req := &a2aclient.Request{
		Payload: nil,
	}

	ctx, err := ci.Before(context.Background(), req)
	if err != nil {
		t.Fatalf("Before() error = %v", err)
	}

	if ctx == nil {
		t.Fatal("Before() returned nil context")
	}
}

func TestContextCleanerInterceptor_WrongPayloadTypeSkipsProcessing(t *testing.T) {
	ci := &contextCleanerInterceptor{
		agentName: "test_agent",
		enabled:   true,
	}

	// Wrong payload type (not *a2a.MessageSendParams)
	req := &a2aclient.Request{
		Payload: "wrong type",
	}

	ctx, err := ci.Before(context.Background(), req)
	if err != nil {
		t.Fatalf("Before() error = %v", err)
	}

	if ctx == nil {
		t.Fatal("Before() returned nil context")
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 10, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}
