package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"

	"knowledge-agent/internal/config"
)

func TestNewA2AToolset_Disabled(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled: false,
	}

	toolset, err := NewA2AToolset(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toolset != nil {
		t.Error("expected nil toolset when A2A is disabled")
	}
}

func TestNewA2AToolset_NoSubAgents(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:   true,
		SelfName:  "test-agent",
		SubAgents: nil,
	}

	toolset, err := NewA2AToolset(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toolset != nil {
		t.Error("expected nil toolset when no sub-agents configured")
	}
}

func TestNewA2AToolset_WithMockAgent(t *testing.T) {
	// Create a mock A2A server that returns an agent card
	// Note: URL will be set dynamically after server starts
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			agentCard := map[string]any{
				"name":               "mock-agent",
				"description":        "A mock agent for testing",
				"url":                serverURL,
				"version":            "1.0.0",
				"protocolVersion":    "0.2.2",
				"preferredTransport": "JSONRPC",
				"capabilities": map[string]any{
					"streaming": false,
				},
				"defaultInputModes":  []string{"text"},
				"defaultOutputModes": []string{"text"},
				"skills": []map[string]any{
					{
						"id":   "test",
						"name": "Test Skill",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(agentCard); err != nil {
				t.Errorf("failed to encode agent card: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	serverURL = server.URL

	cfg := &config.A2AConfig{
		Enabled:  true,
		SelfName: "test-parent-agent",
		Polling:  true,
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:     "mock_agent",
				Endpoint: server.URL,
				Auth: config.A2AAuthConfig{
					Type: "none",
				},
				Timeout: 30,
			},
		},
		QueryExtractor: config.A2AQueryExtractorConfig{
			Enabled: false, // Disable to avoid needing Anthropic API key
		},
	}

	toolset, err := NewA2AToolset(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toolset == nil {
		t.Fatal("expected non-nil toolset")
	}

	// Verify toolset has the expected tool
	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("unexpected error getting tools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if tool.Name() != "query_mock_agent" {
		t.Errorf("expected tool name 'query_mock_agent', got '%s'", tool.Name())
	}

	// Verify description includes agent card description
	if tool.Description() == "" {
		t.Error("expected non-empty tool description")
	}

	// Clean up
	if err := toolset.Close(); err != nil {
		t.Errorf("error closing toolset: %v", err)
	}
}

func TestA2AToolset_Name(t *testing.T) {
	toolset := &A2AToolset{}
	if toolset.Name() != "a2a_toolset" {
		t.Errorf("expected name 'a2a_toolset', got '%s'", toolset.Name())
	}
}

func TestA2AToolset_Tools_Nil(t *testing.T) {
	var toolset *A2AToolset = nil
	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tools != nil {
		t.Error("expected nil tools for nil toolset")
	}
}

func TestA2AToolset_Close_Nil(t *testing.T) {
	var toolset *A2AToolset = nil
	err := toolset.Close()
	if err != nil {
		t.Errorf("unexpected error closing nil toolset: %v", err)
	}
}

func TestExtractTextFromResult_Message(t *testing.T) {
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello, world!"})
	result := extractTextFromResult(msg)
	if result != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got '%s'", result)
	}
}

func TestExtractTextFromResult_MultiPart(t *testing.T) {
	msg := a2a.NewMessage(a2a.MessageRoleAgent,
		a2a.TextPart{Text: "Part 1"},
		a2a.TextPart{Text: "Part 2"},
	)
	result := extractTextFromResult(msg)
	if result != "Part 1\nPart 2" {
		t.Errorf("expected 'Part 1\\nPart 2', got '%s'", result)
	}
}

func TestExtractTextFromResult_Task(t *testing.T) {
	task := &a2a.Task{
		ID: "test-task",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateCompleted,
			Message: a2a.NewMessage(a2a.MessageRoleAgent,
				a2a.TextPart{Text: "Task completed"},
			),
		},
	}
	result := extractTextFromResult(task)
	if result != "Task completed" {
		t.Errorf("expected 'Task completed', got '%s'", result)
	}
}

func TestExtractTextFromResult_Nil(t *testing.T) {
	result := extractTextFromResult(nil)
	if result != "" {
		t.Errorf("expected empty string for nil result, got '%s'", result)
	}
}

func TestQuerySubAgentResult_JSON(t *testing.T) {
	result := QuerySubAgentResult{
		Success:  true,
		Response: "Test response",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded QuerySubAgentResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !decoded.Success || decoded.Response != "Test response" {
		t.Error("JSON round-trip failed")
	}
}

func TestQuerySubAgentArgs_JSON(t *testing.T) {
	args := QuerySubAgentArgs{
		Query: "Test query",
	}

	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded QuerySubAgentArgs
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decoded.Query != "Test query" {
		t.Error("JSON round-trip failed")
	}
}

func TestExtractTextFromResult_Truncation(t *testing.T) {
	// Create a message with text larger than maxResponseTextLength
	largeText := strings.Repeat("x", maxResponseTextLength+1000)
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: largeText})

	result := extractTextFromResult(msg)

	// Should be truncated
	if len(result) > maxResponseTextLength+100 { // +100 for truncation message
		t.Errorf("result should be truncated, got length %d", len(result))
	}

	if !strings.Contains(result, "[TRUNCATED") {
		t.Error("truncated result should contain truncation marker")
	}

	// Verify the truncation is at the right place
	if !strings.HasPrefix(result, strings.Repeat("x", 100)) {
		t.Error("truncated result should start with original content")
	}
}

func TestExtractTextFromResult_NoTruncationForSmallText(t *testing.T) {
	smallText := "This is a small response"
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: smallText})

	result := extractTextFromResult(msg)

	if result != smallText {
		t.Errorf("small text should not be modified, got '%s'", result)
	}

	if strings.Contains(result, "[TRUNCATED") {
		t.Error("small text should not have truncation marker")
	}
}

func TestExtractTextFromResult_ExactlyAtLimit(t *testing.T) {
	// Create text exactly at the limit
	exactText := strings.Repeat("x", maxResponseTextLength)
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: exactText})

	result := extractTextFromResult(msg)

	// Should NOT be truncated (exactly at limit, not over)
	if result != exactText {
		t.Error("text exactly at limit should not be truncated")
	}

	if strings.Contains(result, "[TRUNCATED") {
		t.Error("text exactly at limit should not have truncation marker")
	}
}

// TestSubAgentHandlerInvocation tests the full handler invocation flow with a mock A2A server
func TestSubAgentHandlerInvocation(t *testing.T) {
	var serverURL string
	receivedQuery := ""

	// Create a mock A2A server that:
	// 1. Returns agent card on /.well-known/agent-card.json
	// 2. Handles JSON-RPC requests on root path
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			agentCard := map[string]any{
				"name":               "test-agent",
				"description":        "A test agent",
				"url":                serverURL,
				"version":            "1.0.0",
				"protocolVersion":    "0.2.2",
				"preferredTransport": "JSONRPC",
				"capabilities": map[string]any{
					"streaming": false,
				},
				"defaultInputModes":  []string{"text"},
				"defaultOutputModes": []string{"text"},
				"skills": []map[string]any{
					{"id": "test", "name": "Test"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(agentCard)
			return
		}

		// Handle JSON-RPC requests
		if r.Method == http.MethodPost {
			var jsonRPCReq struct {
				JSONRPC string `json:"jsonrpc"`
				Method  string `json:"method"`
				ID      any    `json:"id"`
				Params  struct {
					Message struct {
						Parts []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"parts"`
					} `json:"message"`
				} `json:"params"`
			}

			if err := json.NewDecoder(r.Body).Decode(&jsonRPCReq); err != nil {
				t.Logf("Failed to decode JSON-RPC request: %v", err)
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}

			// Capture the received query
			if len(jsonRPCReq.Params.Message.Parts) > 0 {
				receivedQuery = jsonRPCReq.Params.Message.Parts[0].Text
			}

			// Return a valid A2A response
			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      jsonRPCReq.ID,
				"result": map[string]any{
					"role": "agent",
					"parts": []map[string]any{
						{
							"type": "text",
							"text": "Mock response for: " + receivedQuery,
						},
					},
				},
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()
	serverURL = server.URL

	// Create toolset
	cfg := &config.A2AConfig{
		Enabled:  true,
		SelfName: "test-parent",
		Polling:  true,
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:     "test_agent",
				Endpoint: server.URL,
				Auth:     config.A2AAuthConfig{Type: "none"},
				Timeout:  5,
			},
		},
		QueryExtractor: config.A2AQueryExtractorConfig{Enabled: false},
	}

	toolset, err := NewA2AToolset(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create toolset: %v", err)
	}
	defer toolset.Close()

	// Get the tool
	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("Failed to get tools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	// Verify tool was created with correct name
	if tools[0].Name() != "query_test_agent" {
		t.Errorf("Expected tool name 'query_test_agent', got '%s'", tools[0].Name())
	}

	// Note: We cannot directly invoke the tool handler without ADK context,
	// but we verified the tool is created correctly and the mock server
	// is ready to handle requests. The actual invocation is tested via
	// integration tests.
	t.Logf("Tool '%s' created successfully with description: %s",
		tools[0].Name(), tools[0].Description())
}

// TestNewA2AToolset_RESTProtocol tests toolset creation with REST protocol
func TestNewA2AToolset_RESTProtocol(t *testing.T) {
	var serverURL string

	// Create a mock server that handles both agent-card and /api/query
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			agentCard := map[string]any{
				"name":               "rest-agent",
				"description":        "A REST-based test agent",
				"url":                serverURL,
				"version":            "1.0.0",
				"protocolVersion":    "0.2.2",
				"preferredTransport": "JSONRPC",
				"capabilities":       map[string]any{"streaming": false},
				"defaultInputModes":  []string{"text"},
				"defaultOutputModes": []string{"text"},
				"skills":             []map[string]any{{"id": "test", "name": "Test"}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(agentCard)
			return
		}

		// Handle REST /query endpoint
		if r.URL.Path == "/query" && r.Method == http.MethodPost {
			var req struct {
				Query     string `json:"query"`
				ChannelID string `json:"channel_id"`
				SessionID string `json:"session_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}

			// Return success response
			resp := map[string]any{
				"success": true,
				"answer":  "REST response for: " + req.Query,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()
	serverURL = server.URL

	cfg := &config.A2AConfig{
		Enabled:  true,
		SelfName: "test-parent",
		Polling:  true,
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:     "rest_agent",
				Endpoint: server.URL,
				Protocol: "rest", // Use REST protocol
				Auth:     config.A2AAuthConfig{Type: "none"},
				Timeout:  5,
			},
		},
		QueryExtractor: config.A2AQueryExtractorConfig{Enabled: false},
	}

	toolset, err := NewA2AToolset(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create toolset: %v", err)
	}
	defer toolset.Close()

	// Get tools
	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("Failed to get tools: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	// Verify tool was created with correct name
	tool := tools[0]
	if tool.Name() != "query_rest_agent" {
		t.Errorf("Expected tool name 'query_rest_agent', got '%s'", tool.Name())
	}

	// Verify description was extracted from agent card
	if !strings.Contains(tool.Description(), "REST-based test agent") {
		t.Errorf("Expected description to contain agent card description, got '%s'", tool.Description())
	}

	t.Logf("REST tool '%s' created successfully with description: %s",
		tool.Name(), tool.Description())
}

// TestNewA2AToolset_MixedProtocols tests toolset with both A2A and REST agents
func TestNewA2AToolset_MixedProtocols(t *testing.T) {
	var a2aServerURL, restServerURL string

	// Create A2A mock server
	a2aServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			agentCard := map[string]any{
				"name":               "a2a-agent",
				"description":        "An A2A protocol agent",
				"url":                a2aServerURL,
				"version":            "1.0.0",
				"protocolVersion":    "0.2.2",
				"preferredTransport": "JSONRPC",
				"capabilities":       map[string]any{"streaming": false},
				"defaultInputModes":  []string{"text"},
				"defaultOutputModes": []string{"text"},
				"skills":             []map[string]any{{"id": "test", "name": "Test"}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(agentCard)
			return
		}
		http.NotFound(w, r)
	}))
	defer a2aServer.Close()
	a2aServerURL = a2aServer.URL

	// Create REST mock server
	restServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			agentCard := map[string]any{
				"name":               "rest-agent",
				"description":        "A REST protocol agent",
				"url":                restServerURL,
				"version":            "1.0.0",
				"protocolVersion":    "0.2.2",
				"preferredTransport": "JSONRPC",
				"capabilities":       map[string]any{"streaming": false},
				"defaultInputModes":  []string{"text"},
				"defaultOutputModes": []string{"text"},
				"skills":             []map[string]any{{"id": "test", "name": "Test"}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(agentCard)
			return
		}
		http.NotFound(w, r)
	}))
	defer restServer.Close()
	restServerURL = restServer.URL

	cfg := &config.A2AConfig{
		Enabled:  true,
		SelfName: "test-parent",
		Polling:  true,
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:     "a2a_agent",
				Endpoint: a2aServer.URL,
				Protocol: "a2a", // Explicit A2A protocol
				Auth:     config.A2AAuthConfig{Type: "none"},
				Timeout:  5,
			},
			{
				Name:     "rest_agent",
				Endpoint: restServer.URL,
				Protocol: "rest", // REST protocol
				Auth:     config.A2AAuthConfig{Type: "none"},
				Timeout:  5,
			},
		},
		QueryExtractor: config.A2AQueryExtractorConfig{Enabled: false},
	}

	toolset, err := NewA2AToolset(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create toolset: %v", err)
	}
	defer toolset.Close()

	// Get tools (should be 3: a2a_agent, rest_agent, and query_multiple_agents)
	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("Failed to get tools: %v", err)
	}

	if len(tools) != 3 {
		t.Fatalf("Expected 3 tools (2 individual + 1 parallel), got %d", len(tools))
	}

	// Verify tool names
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name()] = true
	}

	if !toolNames["query_a2a_agent"] {
		t.Error("Expected tool 'query_a2a_agent' to exist")
	}
	if !toolNames["query_rest_agent"] {
		t.Error("Expected tool 'query_rest_agent' to exist")
	}
	if !toolNames["query_multiple_agents"] {
		t.Error("Expected parallel tool 'query_multiple_agents' to exist")
	}

	t.Logf("Mixed protocol toolset created with %d tools", len(tools))
}

// TestNewA2AToolset_DefaultProtocol tests that default protocol is A2A
func TestNewA2AToolset_DefaultProtocol(t *testing.T) {
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			agentCard := map[string]any{
				"name":               "default-agent",
				"description":        "Agent without explicit protocol",
				"url":                serverURL,
				"version":            "1.0.0",
				"protocolVersion":    "0.2.2",
				"preferredTransport": "JSONRPC",
				"capabilities":       map[string]any{"streaming": false},
				"defaultInputModes":  []string{"text"},
				"defaultOutputModes": []string{"text"},
				"skills":             []map[string]any{{"id": "test", "name": "Test"}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(agentCard)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	serverURL = server.URL

	cfg := &config.A2AConfig{
		Enabled:  true,
		SelfName: "test-parent",
		Polling:  true,
		SubAgents: []config.A2ASubAgentConfig{
			{
				Name:     "default_agent",
				Endpoint: server.URL,
				// Protocol intentionally not set - should default to "a2a"
				Auth:    config.A2AAuthConfig{Type: "none"},
				Timeout: 5,
			},
		},
		QueryExtractor: config.A2AQueryExtractorConfig{Enabled: false},
	}

	toolset, err := NewA2AToolset(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create toolset: %v", err)
	}
	defer toolset.Close()

	// Should have created tool using A2A protocol (default)
	// The tool was created successfully, which means it used A2A
	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("Failed to get tools: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	// Verify an A2A client was created (not REST)
	if len(toolset.a2aClients) != 1 {
		t.Errorf("Expected 1 A2A client (default protocol), got %d", len(toolset.a2aClients))
	}

	t.Logf("Default protocol test passed - A2A client created")
}

// TestTimeoutConstants verifies timeout constants are properly defined
func TestTimeoutConstants(t *testing.T) {
	// Verify constants are reasonable values
	if agentCardResolveTimeout <= 0 {
		t.Error("agentCardResolveTimeout should be positive")
	}

	if defaultHTTPClientTimeout <= 0 {
		t.Error("defaultHTTPClientTimeout should be positive")
	}

	if maxResponseTextLength <= 0 {
		t.Error("maxResponseTextLength should be positive")
	}

	// Verify agent card timeout is less than HTTP client timeout
	if agentCardResolveTimeout >= defaultHTTPClientTimeout {
		t.Error("agentCardResolveTimeout should be less than defaultHTTPClientTimeout")
	}

	// Verify max response text length is reasonable (at least 10KB)
	if maxResponseTextLength < 10_000 {
		t.Error("maxResponseTextLength should be at least 10KB")
	}
}
