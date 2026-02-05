package agent

import (
	"errors"
	"testing"
)

func TestIsOrphanedToolCallError(t *testing.T) {
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
			name:     "orphaned tool_use error (Anthropic format)",
			err:      errors.New("messages.5: `tool_use` ids were found without `tool_result` blocks immediately after"),
			expected: true,
		},
		{
			name:     "tool_use without tool_result",
			err:      errors.New("Error: tool_use was found without matching tool_result"),
			expected: true,
		},
		{
			name:     "partial match - only tool_use",
			err:      errors.New("tool_use call failed"),
			expected: false,
		},
		{
			name:     "partial match - only tool_result",
			err:      errors.New("tool_result missing"),
			expected: false,
		},
		{
			name:     "partial match - only without",
			err:      errors.New("connection timeout without response"),
			expected: false,
		},
		{
			name:     "tool_use and tool_result but no without",
			err:      errors.New("tool_use and tool_result mismatch"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOrphanedToolCallError(tt.err)
			if result != tt.expected {
				t.Errorf("isOrphanedToolCallError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}
