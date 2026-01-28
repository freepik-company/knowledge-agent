package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

func init() {
	logger.Initialize(logger.Config{Level: "error", Format: "console"})
}

func TestNewClient(t *testing.T) {
	cfg := config.A2AAgentConfig{
		Name:     "test-agent",
		Endpoint: "http://localhost:8081",
		Timeout:  30,
		Auth: config.A2AAuthConfig{
			Type: "none",
		},
	}

	client, err := NewClient(cfg, "self-agent")

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if client.agentName != "test-agent" {
		t.Errorf("expected agent name 'test-agent', got '%s'", client.agentName)
	}
	if client.selfName != "self-agent" {
		t.Errorf("expected self name 'self-agent', got '%s'", client.selfName)
	}
}

func TestNewClient_DefaultTimeout(t *testing.T) {
	cfg := config.A2AAgentConfig{
		Name:     "test-agent",
		Endpoint: "http://localhost:8081",
		Timeout:  0, // Should default to 30s
		Auth: config.A2AAuthConfig{
			Type: "none",
		},
	}

	client, err := NewClient(cfg, "self-agent")

	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if client.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", client.timeout)
	}
}

func TestClient_Query_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/query" {
			t.Errorf("expected path /api/query, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}

		// Verify A2A headers
		if r.Header.Get(HeaderRequestID) == "" {
			t.Error("expected X-Request-ID header")
		}
		if r.Header.Get(HeaderCallChain) == "" {
			t.Error("expected X-Call-Chain header")
		}

		// Decode request
		var req QueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Question != "test question" {
			t.Errorf("expected question 'test question', got '%s'", req.Question)
		}

		// Send response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{
			Success: true,
			Answer:  "test answer",
		})
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:     "remote-agent",
		Endpoint: server.URL,
		Timeout:  5,
		Auth:     config.A2AAuthConfig{Type: "none"},
	}

	client, err := NewClient(cfg, "my-agent")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Query(context.Background(), "test question", nil)

	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success to be true")
	}
	if resp.Answer != "test answer" {
		t.Errorf("expected answer 'test answer', got '%s'", resp.Answer)
	}
}

func TestClient_Query_WithAuth(t *testing.T) {
	os.Setenv("TEST_CLIENT_KEY", "secret-key")
	defer os.Unsetenv("TEST_CLIENT_KEY")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("X-API-Key") != "secret-key" {
			t.Errorf("expected X-API-Key 'secret-key', got '%s'", r.Header.Get("X-API-Key"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{Success: true, Answer: "ok"})
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:     "remote-agent",
		Endpoint: server.URL,
		Timeout:  5,
		Auth: config.A2AAuthConfig{
			Type:   "api_key",
			Header: "X-API-Key",
			KeyEnv: "TEST_CLIENT_KEY",
		},
	}

	client, err := NewClient(cfg, "my-agent")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = client.Query(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
}

func TestClient_Query_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{
			Success: false,
			Error:   "something went wrong",
		})
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:     "remote-agent",
		Endpoint: server.URL,
		Timeout:  5,
		Auth:     config.A2AAuthConfig{Type: "none"},
	}

	client, _ := NewClient(cfg, "my-agent")
	_, err := client.Query(context.Background(), "test", nil)

	if err == nil {
		t.Error("expected error for failed response")
	}
}

func TestClient_Query_LoopDetected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(StatusLoopDetected)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "loop detected",
		})
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:     "remote-agent",
		Endpoint: server.URL,
		Timeout:  5,
		Auth:     config.A2AAuthConfig{Type: "none"},
	}

	client, _ := NewClient(cfg, "my-agent")
	_, err := client.Query(context.Background(), "test", nil)

	if err == nil {
		t.Error("expected error for loop detection")
	}
}

func TestClient_Query_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:     "remote-agent",
		Endpoint: server.URL,
		Timeout:  5,
		Auth:     config.A2AAuthConfig{Type: "none"},
	}

	client, _ := NewClient(cfg, "my-agent")
	_, err := client.Query(context.Background(), "test", nil)

	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

func TestValidateA2AEndpoint_Valid(t *testing.T) {
	// A2A is for internal agent communication, so internal IPs are allowed
	validEndpoints := []string{
		"http://logs-agent:8081",
		"https://api.example.com",
		"http://192.168.1.100:8080",
		"http://localhost:8080",      // Allowed for A2A (internal communication)
		"http://127.0.0.1:8080",      // Allowed for A2A (internal communication)
		"http://10.0.0.5:8081",       // Internal network
	}

	for _, endpoint := range validEndpoints {
		if err := validateA2AEndpoint(endpoint); err != nil {
			t.Errorf("expected endpoint %s to be valid, got error: %v", endpoint, err)
		}
	}
}

func TestValidateA2AEndpoint_BlocksMetadata(t *testing.T) {
	// Cloud metadata services must be blocked (SSRF risk)
	blockedEndpoints := []string{
		"http://169.254.169.254/latest/meta-data",
		"http://169.254.1.1/something",  // Link-local range
		"http://metadata.google.internal/computeMetadata",
	}

	for _, endpoint := range blockedEndpoints {
		if err := validateA2AEndpoint(endpoint); err == nil {
			t.Errorf("expected endpoint %s to be blocked", endpoint)
		}
	}
}

func TestValidateA2AEndpoint_BlocksInvalidScheme(t *testing.T) {
	invalidEndpoints := []string{
		"ftp://example.com",
		"file:///etc/passwd",
		"gopher://example.com",
	}

	for _, endpoint := range invalidEndpoints {
		if err := validateA2AEndpoint(endpoint); err == nil {
			t.Errorf("expected endpoint %s with invalid scheme to be blocked", endpoint)
		}
	}
}

func TestNewClient_RejectsUnsafeEndpoint(t *testing.T) {
	cfg := config.A2AAgentConfig{
		Name:     "malicious-agent",
		Endpoint: "http://169.254.169.254/latest/meta-data",
		Auth:     config.A2AAuthConfig{Type: "none"},
	}

	_, err := NewClient(cfg, "self-agent")

	if err == nil {
		t.Error("expected error for metadata service endpoint")
	}
}

func TestClient_Query_PropagatesCallChain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chain := r.Header.Get(HeaderCallChain)
		// Should contain previous agent + self
		if chain != "previous-agent,my-agent" {
			t.Errorf("expected call chain 'previous-agent,my-agent', got '%s'", chain)
		}

		depth := r.Header.Get(HeaderCallDepth)
		if depth != "2" {
			t.Errorf("expected call depth '2', got '%s'", depth)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{Success: true, Answer: "ok"})
	}))
	defer server.Close()

	cfg := config.A2AAgentConfig{
		Name:     "remote-agent",
		Endpoint: server.URL,
		Timeout:  5,
		Auth:     config.A2AAuthConfig{Type: "none"},
	}

	client, _ := NewClient(cfg, "my-agent")

	// Create context with existing call chain
	cc := &CallContext{
		RequestID: "test-req",
		CallChain: []string{"previous-agent"},
		CallDepth: 1,
	}
	ctx := WithCallContext(context.Background(), cc)

	_, err := client.Query(ctx, "test", nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
}
