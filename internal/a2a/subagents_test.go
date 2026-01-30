package a2a

import (
	"testing"

	"knowledge-agent/internal/config"
)

func TestCreateSubAgents_Disabled(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled: false,
		SubAgents: []config.A2ASubAgentConfig{
			{Name: "test", Endpoint: "http://localhost:9000"},
		},
	}

	agents, err := CreateSubAgents(cfg)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if agents != nil {
		t.Errorf("expected nil agents when disabled, got: %v", agents)
	}
}

func TestCreateSubAgents_NoSubAgents(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:   true,
		SelfName:  "test-agent",
		SubAgents: []config.A2ASubAgentConfig{},
	}

	agents, err := CreateSubAgents(cfg)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if agents != nil {
		t.Errorf("expected nil agents when no sub_agents configured, got: %v", agents)
	}
}

func TestCreateSubAgents_NilSubAgents(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:   true,
		SelfName:  "test-agent",
		SubAgents: nil,
	}

	agents, err := CreateSubAgents(cfg)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if agents != nil {
		t.Errorf("expected nil agents when sub_agents is nil, got: %v", agents)
	}
}

func TestCreateSubAgents_AgentCardResolution(t *testing.T) {
	// Now we pre-resolve the agent card at startup (for polling mode)
	// This test requires real A2A servers
	t.Skip("Skipping - requires real A2A servers for agent card resolution")

	cfg := &config.A2AConfig{
		Enabled:  true,
		Polling:  true,
		SelfName: "test-agent",
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:        "test-agent",
				Description: "This will be created with card resolution",
				Endpoint:    "http://some-endpoint:9000",
			},
		},
	}

	agents, err := CreateSubAgents(cfg)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got: %d", len(agents))
	}
}

func TestCreateSubAgents_GracefulDegradation(t *testing.T) {
	// Test that failures in agent card resolution don't crash the system
	// and return empty agents list with graceful degradation
	cfg := &config.A2AConfig{
		Enabled:  true,
		Polling:  true,
		SelfName: "test-agent",
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:        "agent1",
				Description: "First agent (will fail - no server)",
				Endpoint:    "http://non-existent-agent1:9000",
			},
			{
				Name:        "agent2",
				Description: "Second agent (will fail - no server)",
				Endpoint:    "http://non-existent-agent2:9000",
			},
		},
	}

	// Should NOT return an error (graceful degradation)
	agents, err := CreateSubAgents(cfg)

	if err != nil {
		t.Errorf("expected no error (graceful degradation), got: %v", err)
	}
	// No agents should be created (all failed to resolve)
	if len(agents) != 0 {
		t.Errorf("expected 0 agents (graceful degradation), got: %d", len(agents))
	}
}

func TestCreateRemoteAgent_ValidConfig(t *testing.T) {
	// Now that we pre-resolve the agent card, this test will fail without a real server
	// Skip if not running integration tests
	t.Skip("Skipping - requires real A2A server for agent card resolution")

	cfg := config.A2ASubAgentConfig{
		Name:        "test-agent",
		Description: "Test description",
		Endpoint:    "http://some-endpoint:9000",
	}

	agent, err := createRemoteAgent(cfg, true, config.RetryConfig{}) // polling=true

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if agent == nil {
		t.Error("expected non-nil agent")
	}
}

func TestCreateRemoteAgent_EmptyEndpoint(t *testing.T) {
	cfg := config.A2ASubAgentConfig{
		Name:        "test-agent",
		Description: "Test description",
		Endpoint:    "",
	}

	// Empty endpoint should fail during card resolution
	_, err := createRemoteAgent(cfg, true, config.RetryConfig{})

	// Should fail because endpoint is empty
	if err == nil {
		t.Error("expected error for empty endpoint")
	}
}

// Test authentication header resolution
func TestResolveAuthHeader_APIKey(t *testing.T) {
	// Set environment variable
	t.Setenv("TEST_API_KEY", "my-secret-key")

	auth := config.A2AAuthConfig{
		Type:   "api_key",
		Header: "X-API-Key",
		KeyEnv: "TEST_API_KEY",
	}

	headerName, headerValue, err := resolveAuthHeader(auth)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if headerName != "X-API-Key" {
		t.Errorf("expected header name 'X-API-Key', got: %s", headerName)
	}
	if headerValue != "my-secret-key" {
		t.Errorf("expected header value 'my-secret-key', got: %s", headerValue)
	}
}

func TestResolveAuthHeader_Bearer(t *testing.T) {
	// Set environment variable
	t.Setenv("TEST_BEARER_TOKEN", "jwt-token-here")

	auth := config.A2AAuthConfig{
		Type:     "bearer",
		TokenEnv: "TEST_BEARER_TOKEN",
	}

	headerName, headerValue, err := resolveAuthHeader(auth)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if headerName != "Authorization" {
		t.Errorf("expected header name 'Authorization', got: %s", headerName)
	}
	if headerValue != "Bearer jwt-token-here" {
		t.Errorf("expected 'Bearer jwt-token-here', got: %s", headerValue)
	}
}

func TestResolveAuthHeader_MissingEnv(t *testing.T) {
	auth := config.A2AAuthConfig{
		Type:   "api_key",
		Header: "X-API-Key",
		KeyEnv: "NON_EXISTENT_ENV_VAR",
	}

	_, _, err := resolveAuthHeader(auth)

	if err == nil {
		t.Error("expected error for missing env var")
	}
}

func TestResolveAuthHeader_MissingHeader(t *testing.T) {
	t.Setenv("TEST_KEY", "value")

	auth := config.A2AAuthConfig{
		Type:   "api_key",
		Header: "", // Missing header
		KeyEnv: "TEST_KEY",
	}

	_, _, err := resolveAuthHeader(auth)

	if err == nil {
		t.Error("expected error for missing header field")
	}
}

func TestResolveAuthHeader_OAuth2NotSupported(t *testing.T) {
	auth := config.A2AAuthConfig{
		Type: "oauth2",
	}

	_, _, err := resolveAuthHeader(auth)

	if err == nil {
		t.Error("expected error for oauth2 (not supported)")
	}
}

func TestResolveAuthHeader_UnsupportedType(t *testing.T) {
	auth := config.A2AAuthConfig{
		Type: "unknown_auth",
	}

	_, _, err := resolveAuthHeader(auth)

	if err == nil {
		t.Error("expected error for unsupported auth type")
	}
}

func TestCreateRemoteAgent_WithAuth(t *testing.T) {
	// Now that we pre-resolve the agent card, this test requires a real server
	t.Skip("Skipping - requires real A2A server for agent card resolution")

	// Set environment variable
	t.Setenv("TEST_AGENT_KEY", "secret-key-123")

	cfg := config.A2ASubAgentConfig{
		Name:        "auth-agent",
		Description: "Agent with authentication",
		Endpoint:    "http://some-endpoint:9000",
		Auth: config.A2AAuthConfig{
			Type:   "api_key",
			Header: "X-API-Key",
			KeyEnv: "TEST_AGENT_KEY",
		},
	}

	agent, err := createRemoteAgent(cfg, true, config.RetryConfig{}) // polling=true

	// Agent should be created successfully
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if agent == nil {
		t.Error("expected non-nil agent")
	}
}

func TestCreateRemoteAgent_AuthFailure(t *testing.T) {
	// Don't set the environment variable - should fail during auth resolution
	// (before even attempting to resolve agent card)
	cfg := config.A2ASubAgentConfig{
		Name:        "auth-agent",
		Description: "Agent with missing auth",
		Endpoint:    "http://some-endpoint:9000",
		Auth: config.A2AAuthConfig{
			Type:   "api_key",
			Header: "X-API-Key",
			KeyEnv: "MISSING_ENV_VAR_12345",
		},
	}

	agent, err := createRemoteAgent(cfg, true, config.RetryConfig{}) // polling=true

	// Should fail because env var is missing (auth resolution happens before card fetch)
	if err == nil {
		t.Error("expected error for missing auth env var")
	}
	if agent != nil {
		t.Error("expected nil agent on auth failure")
	}
}

// TestCreateSubAgentsConfigValidation tests that config validation catches issues
func TestCreateSubAgentsConfigValidation(t *testing.T) {
	testCases := []struct {
		name        string
		cfg         *config.A2AConfig
		expectNil   bool
		expectError bool
	}{
		{
			name: "disabled config",
			cfg: &config.A2AConfig{
				Enabled: false,
			},
			expectNil:   true,
			expectError: false,
		},
		{
			name: "enabled but empty",
			cfg: &config.A2AConfig{
				Enabled:   true,
				SelfName:  "test",
				SubAgents: []config.A2ASubAgentConfig{},
			},
			expectNil:   true,
			expectError: false,
		},
		{
			name: "enabled with invalid agents",
			cfg: &config.A2AConfig{
				Enabled:  true,
				SelfName: "test",
				SubAgents: []config.A2ASubAgentConfig{
					{Name: "bad", Endpoint: "http://localhost:99999"},
				},
			},
			expectNil:   false, // Returns empty slice, not nil
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			agents, err := CreateSubAgents(tc.cfg)

			if tc.expectError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tc.expectNil && agents != nil {
				t.Errorf("expected nil agents, got: %v", agents)
			}
		})
	}
}
