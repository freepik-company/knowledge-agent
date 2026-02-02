package agent

import (
	"strings"
	"testing"
)

func TestResolveSessionID(t *testing.T) {
	tests := []struct {
		name            string
		clientSessionID string
		channelID       string
		threadTS        string
		wantPrefix      string // Use prefix matching for timestamp-based IDs
		wantExact       string // Use exact matching when possible
	}{
		{
			name:            "client-provided session ID takes precedence",
			clientSessionID: "my-custom-session",
			channelID:       "C01ABC123",
			threadTS:        "1234567890.123456",
			wantExact:       "my-custom-session",
		},
		{
			name:            "client session ID with empty channel and thread",
			clientSessionID: "custom-session-123",
			channelID:       "",
			threadTS:        "",
			wantExact:       "custom-session-123",
		},
		{
			name:            "thread context generates thread-based session ID",
			clientSessionID: "",
			channelID:       "C01ABC123",
			threadTS:        "1234567890.123456",
			wantExact:       "thread-C01ABC123-1234567890.123456",
		},
		{
			name:            "channel only generates channel-based session ID with timestamp",
			clientSessionID: "",
			channelID:       "C01ABC123",
			threadTS:        "",
			wantPrefix:      "channel-C01ABC123-",
		},
		{
			name:            "no context generates api-based session ID with timestamp",
			clientSessionID: "",
			channelID:       "",
			threadTS:        "",
			wantPrefix:      "api-",
		},
		{
			name:            "DM channel with thread",
			clientSessionID: "",
			channelID:       "D01XYZ789",
			threadTS:        "9876543210.654321",
			wantExact:       "thread-D01XYZ789-9876543210.654321",
		},
		{
			name:            "empty thread_ts but has channel",
			clientSessionID: "",
			channelID:       "C99TEST99",
			threadTS:        "",
			wantPrefix:      "channel-C99TEST99-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveSessionID(tt.clientSessionID, tt.channelID, tt.threadTS)

			if tt.wantExact != "" {
				if got != tt.wantExact {
					t.Errorf("resolveSessionID() = %q, want exact %q", got, tt.wantExact)
				}
			} else if tt.wantPrefix != "" {
				if !strings.HasPrefix(got, tt.wantPrefix) {
					t.Errorf("resolveSessionID() = %q, want prefix %q", got, tt.wantPrefix)
				}
				// Verify the suffix is a valid unix timestamp (numeric)
				suffix := strings.TrimPrefix(got, tt.wantPrefix)
				if suffix == "" {
					t.Errorf("resolveSessionID() = %q, missing timestamp suffix after prefix %q", got, tt.wantPrefix)
				}
				// Check that suffix contains only digits
				for _, c := range suffix {
					if c < '0' || c > '9' {
						t.Errorf("resolveSessionID() = %q, suffix %q should be numeric", got, suffix)
						break
					}
				}
			}
		})
	}
}

func TestResolveSessionID_Determinism(t *testing.T) {
	// Client-provided session IDs should be deterministic
	id1 := resolveSessionID("my-session", "C01ABC", "123.456")
	id2 := resolveSessionID("my-session", "C01ABC", "123.456")

	if id1 != id2 {
		t.Errorf("Client-provided session ID should be deterministic: got %q and %q", id1, id2)
	}

	// Thread-based session IDs should be deterministic
	id3 := resolveSessionID("", "C01ABC", "123.456")
	id4 := resolveSessionID("", "C01ABC", "123.456")

	if id3 != id4 {
		t.Errorf("Thread-based session ID should be deterministic: got %q and %q", id3, id4)
	}
}

func TestResolveSessionID_UniqueTimestamps(t *testing.T) {
	// Channel-only and API-only session IDs use timestamps, so they might be the same
	// if called in quick succession. This is acceptable behavior but we test the format.

	// Channel-only format
	channelID := resolveSessionID("", "C01ABC", "")
	if !strings.HasPrefix(channelID, "channel-C01ABC-") {
		t.Errorf("Channel-only session ID format incorrect: %q", channelID)
	}

	// API-only format
	apiID := resolveSessionID("", "", "")
	if !strings.HasPrefix(apiID, "api-") {
		t.Errorf("API-only session ID format incorrect: %q", apiID)
	}
}
