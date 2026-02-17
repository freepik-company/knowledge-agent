package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"knowledge-agent/internal/ctxutil"
)

func TestRESTClient_Query_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/query" {
			t.Errorf("expected /query, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key test-key, got %s", r.Header.Get("X-API-Key"))
		}

		// Parse request body
		var req QueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.Query != "What is the weather?" {
			t.Errorf("expected question 'What is the weather?', got '%s'", req.Query)
		}

		// Return success response (simple format)
		resp := map[string]any{
			"success": true,
			"answer":  "The weather is sunny.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client
	client := NewRESTClient(RESTClientConfig{
		Name:           "test-agent",
		BaseURL:        server.URL,
		Timeout:        10 * time.Second,
		AuthHeaderName: "X-API-Key",
		AuthHeaderVal:  "test-key",
	})

	// Execute query
	resp, err := client.Query(context.Background(), "What is the weather?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.IsSuccess() {
		t.Error("expected success=true")
	}
	if resp.GetAnswer() != "The weather is sunny." {
		t.Errorf("expected answer 'The weather is sunny.', got '%s'", resp.GetAnswer())
	}
}

func TestRESTClient_Query_ADKFormat(t *testing.T) {
	// Create mock server that returns ADK format (agents built with Google ADK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return ADK format response (response field instead of answer)
		resp := map[string]interface{}{
			"response":        "Found 15 errors in the last hour.",
			"conversation_id": "session-123",
			"model":           "claude-sonnet-4-5",
			"tool_calls":      []interface{}{},
			"tool_results":    []interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client
	client := NewRESTClient(RESTClientConfig{
		Name:    "logs-agent",
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	})

	// Execute query
	resp, err := client.Query(context.Background(), "Show me errors")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ADK format is handled correctly
	if !resp.IsSuccess() {
		t.Error("expected IsSuccess()=true for ADK format")
	}
	if resp.GetAnswer() != "Found 15 errors in the last hour." {
		t.Errorf("expected GetAnswer() to return response field, got '%s'", resp.GetAnswer())
	}
	// Note: ConversationID and Model are not exposed by the agnostic parser
	// The LLM receives them as part of the response if needed
}

func TestRESTClient_Query_PropagatesIdentity(t *testing.T) {
	var capturedHeaders http.Header

	// Create mock server that captures headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()

		resp := map[string]any{"success": true, "answer": "OK"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client
	client := NewRESTClient(RESTClientConfig{
		Name:    "test-agent",
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	})

	// Create context with identity
	ctx := context.Background()
	ctx = context.WithValue(ctx, ctxutil.UserEmailKey, "user@example.com")
	ctx = context.WithValue(ctx, ctxutil.SlackUserIDKey, "U123456")
	ctx = context.WithValue(ctx, ctxutil.CallerIDKey, "test-caller")
	ctx = context.WithValue(ctx, ctxutil.SessionIDKey, "session-123")
	ctx = context.WithValue(ctx, ctxutil.UserGroupsKey, []string{"group1", "group2"})

	// Execute query
	_, err := client.Query(ctx, "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify headers were propagated
	if capturedHeaders.Get(HeaderUserEmail) != "user@example.com" {
		t.Errorf("expected X-User-Email=user@example.com, got %s", capturedHeaders.Get(HeaderUserEmail))
	}
	if capturedHeaders.Get(HeaderUserID) != "user@example.com" {
		t.Errorf("expected X-User-ID=user@example.com, got %s", capturedHeaders.Get(HeaderUserID))
	}
	if capturedHeaders.Get(HeaderSlackUserID) != "U123456" {
		t.Errorf("expected X-Slack-User-Id=U123456, got %s", capturedHeaders.Get(HeaderSlackUserID))
	}
	if capturedHeaders.Get(HeaderCallerID) != "test-caller" {
		t.Errorf("expected X-Caller-Id=test-caller, got %s", capturedHeaders.Get(HeaderCallerID))
	}
	// NOTE: X-Session-Id is intentionally NOT propagated to REST sub-agents
	// because some agents interpret it as an existing session to validate/resume

	// Verify groups are JSON encoded
	groupsJSON := capturedHeaders.Get(HeaderUserGroups)
	var groups []string
	if err := json.Unmarshal([]byte(groupsJSON), &groups); err != nil {
		t.Errorf("failed to parse groups JSON: %v", err)
	}
	if len(groups) != 2 || groups[0] != "group1" || groups[1] != "group2" {
		t.Errorf("expected groups [group1, group2], got %v", groups)
	}
}

func TestRESTClient_Query_HTTPError(t *testing.T) {
	// Create mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	// Create client
	client := NewRESTClient(RESTClientConfig{
		Name:    "test-agent",
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	})

	// Execute query
	_, err := client.Query(context.Background(), "test query")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify error message contains useful information
	if err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

func TestRESTClient_Query_Timeout(t *testing.T) {
	// Create mock server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		resp := map[string]any{"success": true, "answer": "OK"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client with short timeout
	client := NewRESTClient(RESTClientConfig{
		Name:    "test-agent",
		BaseURL: server.URL,
		Timeout: 50 * time.Millisecond,
	})

	// Execute query - should timeout
	_, err := client.Query(context.Background(), "test query")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestRESTClient_EndpointNormalization(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		apiPath  string
		expected string
	}{
		{
			name:     "default path",
			baseURL:  "http://agent:8081",
			apiPath:  "",
			expected: "http://agent:8081/query",
		},
		{
			name:     "base URL with trailing slash",
			baseURL:  "http://agent:8081/",
			apiPath:  "",
			expected: "http://agent:8081/query",
		},
		{
			name:     "custom path /api/query",
			baseURL:  "http://agent:8081",
			apiPath:  "/api/query",
			expected: "http://agent:8081/api/query",
		},
		{
			name:     "custom path without leading slash",
			baseURL:  "http://agent:8081",
			apiPath:  "custom/endpoint",
			expected: "http://agent:8081/custom/endpoint",
		},
		{
			name:     "explicit /query path",
			baseURL:  "http://agent:8081",
			apiPath:  "/query",
			expected: "http://agent:8081/query",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewRESTClient(RESTClientConfig{
				Name:    "test",
				BaseURL: tt.baseURL,
				APIPath: tt.apiPath,
				Timeout: time.Second,
			})
			if client.endpoint != tt.expected {
				t.Errorf("expected endpoint %s, got %s", tt.expected, client.endpoint)
			}
		})
	}
}

func TestExtractBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		fullURL  string
		expected string
		wantErr  bool
	}{
		{
			name:     "full URL with path",
			fullURL:  "http://agent:8081/a2a/invoke",
			expected: "http://agent:8081",
		},
		{
			name:     "URL without path",
			fullURL:  "http://agent:8081",
			expected: "http://agent:8081",
		},
		{
			name:     "HTTPS URL",
			fullURL:  "https://agent.example.com:9000/some/path",
			expected: "https://agent.example.com:9000",
		},
		{
			name:     "URL without port",
			fullURL:  "http://agent.example.com/path",
			expected: "http://agent.example.com",
		},
		{
			name:    "invalid URL",
			fullURL: "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractBaseURL(tt.fullURL)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestRESTClient_Query_BodyFormat(t *testing.T) {
	var capturedBody QueryRequest

	// Create mock server that captures request body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)

		resp := map[string]any{"success": true, "answer": "OK"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client
	client := NewRESTClient(RESTClientConfig{
		Name:    "test-agent",
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	})

	// Execute query
	_, err := client.Query(context.Background(), "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify body format: query and channel_id only (no session_id)
	if capturedBody.Query != "test query" {
		t.Errorf("expected query='test query', got '%s'", capturedBody.Query)
	}
	if capturedBody.ChannelID != "a2a-rest" {
		t.Errorf("expected channel_id=a2a-rest, got %s", capturedBody.ChannelID)
	}
	// session_id should NOT be sent in body (sub-agents create their own sessions)
	if capturedBody.SessionID != "" {
		t.Errorf("expected empty session_id, got %s", capturedBody.SessionID)
	}
}

// TestParseQueryResponse_KnownFormats tests parsing of known response formats
func TestParseQueryResponse_KnownFormats(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		expectSuccess bool
		expectAnswer  string
		expectError   string
	}{
		{
			name:          "simple format with answer",
			body:          `{"success": true, "answer": "Hello world"}`,
			expectSuccess: true,
			expectAnswer:  "Hello world",
		},
		{
			name:          "ADK format with response",
			body:          `{"response": "Found 5 results", "model": "claude-3"}`,
			expectSuccess: true,
			expectAnswer:  "Found 5 results",
		},
		{
			name:          "text field format",
			body:          `{"text": "Some text response"}`,
			expectSuccess: true,
			expectAnswer:  "Some text response",
		},
		{
			name:          "result field format",
			body:          `{"result": "Operation completed"}`,
			expectSuccess: true,
			expectAnswer:  "Operation completed",
		},
		{
			name:          "output field format",
			body:          `{"output": "Command output here"}`,
			expectSuccess: true,
			expectAnswer:  "Command output here",
		},
		{
			name:          "content field format",
			body:          `{"content": "Content data"}`,
			expectSuccess: true,
			expectAnswer:  "Content data",
		},
		{
			name:          "nested data.text format",
			body:          `{"data": {"text": "Nested response"}}`,
			expectSuccess: true,
			expectAnswer:  "Nested response",
		},
		{
			name:          "error field indicates failure",
			body:          `{"error": "Something went wrong"}`,
			expectSuccess: false,
			expectError:   "Something went wrong",
		},
		{
			name:          "success false",
			body:          `{"success": false, "message": "Failed"}`,
			expectSuccess: false,
			expectAnswer:  "Failed", // message is a known field
		},
		{
			name:          "plain text (not JSON)",
			body:          `This is plain text response`,
			expectSuccess: true,
			expectAnswer:  "This is plain text response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := ParseQueryResponse([]byte(tt.body))

			if resp.IsSuccess() != tt.expectSuccess {
				t.Errorf("IsSuccess() = %v, want %v", resp.IsSuccess(), tt.expectSuccess)
			}
			if tt.expectAnswer != "" && resp.GetAnswer() != tt.expectAnswer {
				t.Errorf("GetAnswer() = %q, want %q", resp.GetAnswer(), tt.expectAnswer)
			}
			if tt.expectError != "" && resp.GetError() != tt.expectError {
				t.Errorf("GetError() = %q, want %q", resp.GetError(), tt.expectError)
			}
		})
	}
}

// TestParseQueryResponse_UnknownFormat tests that unknown formats are serialized as JSON
func TestParseQueryResponse_UnknownFormat(t *testing.T) {
	// A response with no known fields should be serialized as formatted JSON
	body := `{"custom_field": "value", "another": 123, "nested": {"foo": "bar"}}`
	resp := ParseQueryResponse([]byte(body))

	if !resp.IsSuccess() {
		t.Error("expected success for unknown format")
	}

	answer := resp.GetAnswer()
	// The answer should be formatted JSON
	if answer == "" {
		t.Error("expected non-empty answer")
	}

	// Verify it's valid JSON and contains expected fields
	var parsed map[string]any
	if err := json.Unmarshal([]byte(answer), &parsed); err != nil {
		t.Errorf("answer should be valid JSON: %v", err)
	}
	if parsed["custom_field"] != "value" {
		t.Error("expected custom_field to be preserved")
	}
}

// TestParseQueryResponse_PriorityOrder tests that answer fields are checked in priority order
func TestParseQueryResponse_PriorityOrder(t *testing.T) {
	// If both "answer" and "response" exist, "answer" should take priority
	body := `{"answer": "From answer", "response": "From response"}`
	resp := ParseQueryResponse([]byte(body))

	if resp.GetAnswer() != "From answer" {
		t.Errorf("expected 'From answer' (higher priority), got %q", resp.GetAnswer())
	}
}
