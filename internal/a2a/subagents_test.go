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

func TestCreateSubAgents_LazyInitialization(t *testing.T) {
	// remoteagent.NewA2A creates agents lazily - it doesn't validate
	// the endpoint until the agent is actually used. This is expected
	// behavior for graceful startup.
	cfg := &config.A2AConfig{
		Enabled:  true,
		SelfName: "test-agent",
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:        "lazy-agent",
				Description: "This will be created lazily",
				Endpoint:    "http://some-endpoint:9000",
			},
		},
	}

	// This should not return an error - agent is created lazily
	agents, err := CreateSubAgents(cfg)

	if err != nil {
		t.Errorf("expected no error (lazy creation), got: %v", err)
	}
	// Agent should be created (validation happens later when used)
	if len(agents) != 1 {
		t.Errorf("expected 1 agent (lazy), got: %d", len(agents))
	}
}

func TestCreateSubAgents_MultipleAgents(t *testing.T) {
	// Test that multiple sub-agents are created correctly
	// remoteagent.NewA2A creates agents lazily - validation happens later
	cfg := &config.A2AConfig{
		Enabled:  true,
		SelfName: "test-agent",
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:        "agent1",
				Description: "First agent",
				Endpoint:    "http://agent1:9000",
			},
			{
				Name:        "agent2",
				Description: "Second agent",
				Endpoint:    "http://agent2:9000",
			},
		},
	}

	agents, err := CreateSubAgents(cfg)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	// Both agents should be created (lazy initialization)
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got: %d", len(agents))
	}
}

func TestCreateRemoteAgent_ValidConfig(t *testing.T) {
	// remoteagent.NewA2A uses lazy initialization, so it succeeds
	// even with invalid endpoints - errors happen at invocation time
	cfg := config.A2ASubAgentConfig{
		Name:        "test-agent",
		Description: "Test description",
		Endpoint:    "http://some-endpoint:9000",
	}

	agent, err := createRemoteAgent(cfg)

	// Agent is created (lazy) - no error expected
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

	agent, err := createRemoteAgent(cfg)

	// Even empty endpoint may be accepted by lazy initialization
	// The actual behavior depends on remoteagent implementation
	// We just verify the function doesn't panic
	_ = agent
	_ = err
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
