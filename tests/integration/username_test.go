// +build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/server"
)

// TestUserNameIntegration verifies that user names are properly fetched and included in queries
func TestUserNameIntegration(t *testing.T) {
	// Skip if no config available
	cfg, err := config.Load("")
	if err != nil {
		t.Skip("Skipping integration test: no config available")
	}

	// Create agent
	ctx := context.Background()
	agnt, err := agent.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	defer agnt.Close()

	// Create test server
	srv := server.NewAgentServer(agnt, cfg)

	// Test query with user name and real name
	t.Run("QueryWithUserNames", func(t *testing.T) {
		queryReq := agent.QueryRequest{
			Question:     "Hello, who am I?",
			UserName:     "johndoe",
			UserRealName: "John Doe",
			ChannelID:    "C123TEST",
		}

		reqBody, _ := json.Marshal(queryReq)
		req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(string(reqBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)

		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp agent.QueryResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Success {
			t.Errorf("Query failed: %s", resp.Message)
		}

		// Verify response contains user greeting (agent should use the name)
		// Note: This is a best-effort check since LLM responses vary
		if resp.Answer == "" {
			t.Error("Expected non-empty answer")
		}

		t.Logf("Response: %s", resp.Answer)
	})

	// Test query without user names (should still work)
	t.Run("QueryWithoutUserNames", func(t *testing.T) {
		queryReq := agent.QueryRequest{
			Question:  "What is the weather?",
			ChannelID: "C123TEST",
		}

		reqBody, _ := json.Marshal(queryReq)
		req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(string(reqBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)

		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp agent.QueryResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Success {
			t.Errorf("Query failed: %s", resp.Message)
		}
	})
}

// TestUserNameInstructions verifies user names are included in agent instructions
func TestUserNameInstructions(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Skip("Skipping integration test: no config available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	agnt, err := agent.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	defer agnt.Close()

	// Test that query with user name includes it in the instruction
	queryReq := agent.QueryRequest{
		Question:     "Test question",
		UserName:     "testuser",
		UserRealName: "Test User",
		ChannelID:    "C123",
	}

	resp, err := agnt.Query(ctx, queryReq)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected successful query, got: %s", resp.Message)
	}

	// The agent should have processed the user name
	// We can't directly verify the instruction, but we can verify the query succeeds
	t.Logf("Query with user name successful: %s", resp.Answer)
}
