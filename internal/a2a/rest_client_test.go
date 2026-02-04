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

		// Return success response
		resp := QueryResponse{
			Success: true,
			Answer:  "The weather is sunny.",
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

	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Answer != "The weather is sunny." {
		t.Errorf("expected answer 'The weather is sunny.', got '%s'", resp.Answer)
	}
}

func TestRESTClient_Query_PropagatesIdentity(t *testing.T) {
	var capturedHeaders http.Header

	// Create mock server that captures headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()

		resp := QueryResponse{Success: true, Answer: "OK"}
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
		resp := QueryResponse{Success: true, Answer: "OK"}
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

		resp := QueryResponse{Success: true, Answer: "OK"}
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
