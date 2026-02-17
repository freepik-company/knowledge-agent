package agent

import (
	"errors"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"short", "hello", 1},
		{"typical", "This is a typical sentence with some words.", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.text)
			if got != tt.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestBuildTextFromEvents(t *testing.T) {
	tests := []struct {
		name   string
		events []*session.Event
		want   string
	}{
		{
			name:   "empty events",
			events: nil,
			want:   "",
		},
		{
			name: "single user event",
			events: []*session.Event{
				{LLMResponse: newLLMResponseFromText("user", "hello")},
			},
			want: "[user]: hello\n",
		},
		{
			name: "user and model events",
			events: []*session.Event{
				{LLMResponse: newLLMResponseFromText("user", "question")},
				{LLMResponse: newLLMResponseFromText("model", "answer")},
			},
			want: "[user]: question\n[model]: answer\n",
		},
		{
			name: "event with nil content is skipped",
			events: []*session.Event{
				{LLMResponse: newLLMResponseFromText("user", "hello")},
				{}, // nil content
				{LLMResponse: newLLMResponseFromText("model", "world")},
			},
			want: "[user]: hello\n[model]: world\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTextFromEvents(tt.events)
			if got != tt.want {
				t.Errorf("buildTextFromEvents() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsContextOverflowError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("something went wrong"),
			expected: false,
		},
		{
			name:     "prompt is too long",
			err:      errors.New("Error: prompt is too long, max 200000 tokens"),
			expected: true,
		},
		{
			name:     "too many tokens",
			err:      errors.New("Request has too many tokens"),
			expected: true,
		},
		{
			name:     "maximum context length",
			err:      errors.New("This model's maximum context length is 200000 tokens"),
			expected: true,
		},
		{
			name:     "content too large",
			err:      errors.New("content too large for the model"),
			expected: true,
		},
		{
			name:     "unrelated error with token word",
			err:      errors.New("invalid authentication token"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isContextOverflowError(tt.err)
			if result != tt.expected {
				t.Errorf("isContextOverflowError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

// newLLMResponseFromText is a helper to create an LLMResponse with text content.
func newLLMResponseFromText(role, text string) model.LLMResponse {
	return model.LLMResponse{
		Content: &genai.Content{
			Role:  role,
			Parts: []*genai.Part{genai.NewPartFromText(text)},
		},
	}
}
