package a2a

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

func init() {
	logger.Initialize(logger.Config{Level: "error", Format: "console"})
}

func TestNewToolset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{Success: true, Answer: "ok"})
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:        "test-agent",
		Description: "A test agent",
		Endpoint:    server.URL,
		Timeout:     5,
		Auth:        config.A2AAuthConfig{Type: "none"},
		Tools: []config.A2AToolConfig{
			{Name: "tool1", Description: "First tool"},
			{Name: "tool2", Description: "Second tool"},
		},
	}

	ts, err := NewToolset(cfg, "self-agent")

	if err != nil {
		t.Fatalf("failed to create toolset: %v", err)
	}
	if ts.agentName != "test-agent" {
		t.Errorf("expected agent name 'test-agent', got '%s'", ts.agentName)
	}
	if len(ts.tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(ts.tools))
	}
}

func TestToolset_Name(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{Success: true, Answer: "ok"})
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:     "my-agent",
		Endpoint: server.URL,
		Auth:     config.A2AAuthConfig{Type: "none"},
		Tools:    []config.A2AToolConfig{{Name: "tool1", Description: "desc"}},
	}

	ts, _ := NewToolset(cfg, "self")

	if ts.Name() != "a2a_my-agent_toolset" {
		t.Errorf("expected toolset name 'a2a_my-agent_toolset', got '%s'", ts.Name())
	}
}

func TestToolset_Tools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{Success: true, Answer: "ok"})
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:     "test-agent",
		Endpoint: server.URL,
		Auth:     config.A2AAuthConfig{Type: "none"},
		Tools: []config.A2AToolConfig{
			{Name: "search", Description: "Search for something"},
		},
	}

	ts, _ := NewToolset(cfg, "self")

	tools, err := ts.Tools(nil)
	if err != nil {
		t.Fatalf("failed to get tools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
}

func TestCreateA2AToolsets_Disabled(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled: false,
	}

	toolsets, err := CreateA2AToolsets(cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toolsets) != 0 {
		t.Errorf("expected empty toolsets when disabled, got %d", len(toolsets))
	}
}

func TestCreateA2AToolsets_MissingSelfName(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:  true,
		SelfName: "",
	}

	_, err := CreateA2AToolsets(cfg)

	if err == nil {
		t.Error("expected error when self_name is missing")
	}
}

func TestCreateA2AToolsets_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{Success: true, Answer: "ok"})
	}))
	defer server.Close()

	cfg := &config.A2AConfig{
		Enabled:      true,
		SelfName:     "knowledge-agent",
		MaxCallDepth: 5,
		Agents: []config.A2AAgentConfig{
			{
				Name:     "agent1",
				Endpoint: server.URL,
				Auth:     config.A2AAuthConfig{Type: "none"},
				Tools:    []config.A2AToolConfig{{Name: "tool1", Description: "desc1"}},
			},
			{
				Name:     "agent2",
				Endpoint: server.URL,
				Auth:     config.A2AAuthConfig{Type: "none"},
				Tools:    []config.A2AToolConfig{{Name: "tool2", Description: "desc2"}},
			},
		},
	}

	toolsets, err := CreateA2AToolsets(cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toolsets) != 2 {
		t.Errorf("expected 2 toolsets, got %d", len(toolsets))
	}
}

func TestCreateA2AToolsets_GracefulDegradation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{Success: true, Answer: "ok"})
	}))
	defer server.Close()

	cfg := &config.A2AConfig{
		Enabled:      true,
		SelfName:     "knowledge-agent",
		MaxCallDepth: 5,
		Agents: []config.A2AAgentConfig{
			{
				Name:     "good-agent",
				Endpoint: server.URL,
				Auth:     config.A2AAuthConfig{Type: "none"},
				Tools:    []config.A2AToolConfig{{Name: "tool1", Description: "desc1"}},
			},
			{
				Name:     "bad-agent",
				Endpoint: server.URL,
				Auth: config.A2AAuthConfig{
					Type:   "api_key",
					KeyEnv: "MISSING_ENV_VAR", // Will fail
					Header: "X-API-Key",
				},
				Tools: []config.A2AToolConfig{{Name: "tool2", Description: "desc2"}},
			},
		},
	}

	toolsets, err := CreateA2AToolsets(cfg)

	// Should not return error, just skip the failed agent
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toolsets) != 1 {
		t.Errorf("expected 1 toolset (graceful degradation), got %d", len(toolsets))
	}
}
