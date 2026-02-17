package agent

import (
	"testing"
)

func TestGroupMessagesByRole(t *testing.T) {
	tests := []struct {
		name     string
		messages []map[string]any
		want     []messageGroup
	}{
		{
			name:     "empty messages",
			messages: nil,
			want:     nil,
		},
		{
			name: "single user message",
			messages: []map[string]any{
				{"user_name": "alice", "text": "hello", "ts": "1234567890.000001"},
			},
			want: []messageGroup{
				{role: "user", text: "[alice]: hello"},
			},
		},
		{
			name: "single bot message",
			messages: []map[string]any{
				{"user_name": "bot:assistant", "text": "hi there", "ts": "1234567890.000001"},
			},
			want: []messageGroup{
				{role: "model", text: "hi there"},
			},
		},
		{
			name: "alternating user and bot",
			messages: []map[string]any{
				{"user_name": "alice", "text": "question?", "ts": "1234567890.000001"},
				{"user_name": "bot:assistant", "text": "answer!", "ts": "1234567890.000002"},
				{"user_name": "alice", "text": "thanks", "ts": "1234567890.000003"},
			},
			want: []messageGroup{
				{role: "user", text: "[alice]: question?"},
				{role: "model", text: "answer!"},
				{role: "user", text: "[alice]: thanks"},
			},
		},
		{
			name: "consecutive user messages merge",
			messages: []map[string]any{
				{"user_name": "alice", "text": "first", "ts": "1234567890.000001"},
				{"user_name": "bob", "text": "second", "ts": "1234567890.000002"},
			},
			want: []messageGroup{
				{role: "user", text: "[alice]: first\n[bob]: second"},
			},
		},
		{
			name: "consecutive bot messages merge",
			messages: []map[string]any{
				{"user_name": "bot:assistant", "text": "part 1", "ts": "1234567890.000001"},
				{"user_name": "bot:helper", "text": "part 2", "ts": "1234567890.000002"},
			},
			want: []messageGroup{
				{role: "model", text: "part 1\npart 2"},
			},
		},
		{
			name: "fallback to user field when user_name is empty",
			messages: []map[string]any{
				{"user": "U12345", "text": "hello", "ts": "1234567890.000001"},
			},
			want: []messageGroup{
				{role: "user", text: "[U12345]: hello"},
			},
		},
		{
			name: "unknown user when both fields empty",
			messages: []map[string]any{
				{"text": "anonymous", "ts": "1234567890.000001"},
			},
			want: []messageGroup{
				{role: "user", text: "[Unknown]: anonymous"},
			},
		},
		{
			name: "skip messages with empty text",
			messages: []map[string]any{
				{"user_name": "alice", "text": "", "ts": "1234567890.000001"},
				{"user_name": "bob", "text": "valid", "ts": "1234567890.000002"},
			},
			want: []messageGroup{
				{role: "user", text: "[bob]: valid"},
			},
		},
		{
			name: "complex thread with mixed roles",
			messages: []map[string]any{
				{"user_name": "alice", "text": "what is X?", "ts": "1234567890.000001"},
				{"user_name": "bob", "text": "I also want to know", "ts": "1234567890.000002"},
				{"user_name": "bot:assistant", "text": "X is ...", "ts": "1234567890.000003"},
				{"user_name": "alice", "text": "thanks!", "ts": "1234567890.000004"},
				{"user_name": "bot:assistant", "text": "you're welcome", "ts": "1234567890.000005"},
			},
			want: []messageGroup{
				{role: "user", text: "[alice]: what is X?\n[bob]: I also want to know"},
				{role: "model", text: "X is ..."},
				{role: "user", text: "[alice]: thanks!"},
				{role: "model", text: "you're welcome"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupMessagesByRole(tt.messages)

			if len(got) != len(tt.want) {
				t.Fatalf("groupMessagesByRole() returned %d groups, want %d", len(got), len(tt.want))
			}

			for i := range got {
				if got[i].role != tt.want[i].role {
					t.Errorf("group[%d].role = %q, want %q", i, got[i].role, tt.want[i].role)
				}
				if got[i].text != tt.want[i].text {
					t.Errorf("group[%d].text = %q, want %q", i, got[i].text, tt.want[i].text)
				}
			}
		})
	}
}
