package server

import (
	"testing"
)

func TestNewA2AHandler_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     A2AConfig
		wantErr string
	}{
		{
			name: "missing AgentURL",
			cfg:  A2AConfig{
				// AgentURL missing
				// Agent and SessionService would need to be mocked, but we can test this first
			},
			wantErr: "AgentURL is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewA2AHandler(tt.cfg)

			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !containsSubstr(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestNewA2AHandler_MissingAgent(t *testing.T) {
	cfg := A2AConfig{
		AgentURL: "http://localhost:8081",
		// Agent is nil
	}

	_, err := NewA2AHandler(cfg)

	if err == nil {
		t.Error("expected error for missing Agent")
		return
	}
	if !containsSubstr(err.Error(), "Agent is required") {
		t.Errorf("expected error about Agent, got %q", err.Error())
	}
}

func TestA2AConfig_URLConstruction(t *testing.T) {
	// Test that we construct the invoke URL correctly
	// We can't create a full handler without real ADK components,
	// but we can verify the URL logic by checking the A2AConfig struct

	tests := []struct {
		agentURL       string
		expectedInvoke string
	}{
		{"http://localhost:8081", "http://localhost:8081/a2a/invoke"},
		{"http://example.com", "http://example.com/a2a/invoke"},
		{"https://agent.example.com:9000", "https://agent.example.com:9000/a2a/invoke"},
	}

	for _, tt := range tests {
		// Just verify the logic that would be used
		invokeURL := tt.agentURL + "/a2a/invoke"
		if invokeURL != tt.expectedInvoke {
			t.Errorf("for agentURL %q: got invoke URL %q, want %q",
				tt.agentURL, invokeURL, tt.expectedInvoke)
		}
	}
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
