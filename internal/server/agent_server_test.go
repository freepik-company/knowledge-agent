package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/config"
)

// mockAgentService implements AgentInterface for testing
type mockAgentService struct {
	ingestResponse *agent.IngestResponse
	ingestError    error
	queryResponse  *agent.QueryResponse
	queryError     error
}

func (m *mockAgentService) IngestThread(ctx context.Context, req agent.IngestRequest) (*agent.IngestResponse, error) {
	if m.ingestError != nil {
		return nil, m.ingestError
	}
	if m.ingestResponse != nil {
		return m.ingestResponse, nil
	}
	return &agent.IngestResponse{
		Success:       true,
		Message:       "Thread processed successfully",
		MemoriesAdded: 1,
	}, nil
}

func (m *mockAgentService) Query(ctx context.Context, req agent.QueryRequest) (*agent.QueryResponse, error) {
	if m.queryError != nil {
		return nil, m.queryError
	}
	if m.queryResponse != nil {
		return m.queryResponse, nil
	}
	return &agent.QueryResponse{
		Success: true,
		Answer:  "This is a test answer",
	}, nil
}

func (m *mockAgentService) Close() error {
	return nil
}

func newTestServer(agnt AgentInterface, cfg *config.Config) *AgentServer {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return NewAgentServer(agnt, cfg)
}

func TestAgentServer_Handler(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)

	handler := server.Handler()
	if handler == nil {
		t.Error("Handler() should not return nil")
	}
}

func TestAgentServer_HandleQuery_Success(t *testing.T) {
	mockAgent := &mockAgentService{
		queryResponse: &agent.QueryResponse{
			Success: true,
			Answer:  "Test answer",
		},
	}
	server := newTestServer(mockAgent, nil)
	defer server.Close()

	body := `{"question": "What is the meaning of life?"}`
	req := httptest.NewRequest("POST", "/api/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response agent.QueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !response.Success {
		t.Error("expected success=true")
	}
	if response.Answer != "Test answer" {
		t.Errorf("got answer %q, want %q", response.Answer, "Test answer")
	}
}

func TestAgentServer_HandleQuery_MethodNotAllowed(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	req := httptest.NewRequest("GET", "/api/query", nil)
	rec := httptest.NewRecorder()

	server.handleQuery(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAgentServer_HandleQuery_InvalidJSON(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	req := httptest.NewRequest("POST", "/api/query", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAgentServer_HandleQuery_MissingQuestion(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	body := `{"channel_id": "C123"}`
	req := httptest.NewRequest("POST", "/api/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAgentServer_HandleQuery_RequestBodyTooLarge(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	// Create a body larger than MaxRequestBodySize (1MB)
	largeBody := strings.Repeat("x", MaxRequestBodySize+1)
	body := `{"question": "` + largeBody + `"}`

	req := httptest.NewRequest("POST", "/api/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleQuery(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestAgentServer_HandleIngestThread_Success(t *testing.T) {
	mockAgent := &mockAgentService{}
	server := newTestServer(mockAgent, nil)
	defer server.Close()

	body := `{
		"thread_ts": "1234567890.123456",
		"channel_id": "C123",
		"messages": [{"text": "hello", "user": "U123"}]
	}`
	req := httptest.NewRequest("POST", "/api/ingest-thread", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleIngestThread(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d. body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response agent.IngestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !response.Success {
		t.Error("expected success=true")
	}
}

func TestAgentServer_HandleIngestThread_MethodNotAllowed(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	req := httptest.NewRequest("GET", "/api/ingest-thread", nil)
	rec := httptest.NewRecorder()

	server.handleIngestThread(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAgentServer_HandleIngestThread_MissingRequiredFields(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing thread_ts",
			body: `{"channel_id": "C123", "messages": [{"text": "hi"}]}`,
		},
		{
			name: "missing channel_id",
			body: `{"thread_ts": "123.456", "messages": [{"text": "hi"}]}`,
		},
		{
			name: "empty messages",
			body: `{"thread_ts": "123.456", "channel_id": "C123", "messages": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/ingest-thread", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			server.handleIngestThread(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("got status %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestAgentServer_HandleIngestThread_RequestBodyTooLarge(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	// Create messages that exceed MaxRequestBodySize
	var messages []map[string]string
	for i := 0; i < 10000; i++ {
		messages = append(messages, map[string]string{
			"text": strings.Repeat("x", 200),
			"user": "U123",
		})
	}

	body := map[string]any{
		"thread_ts":  "123.456",
		"channel_id": "C123",
		"messages":   messages,
	}
	bodyBytes, _ := json.Marshal(body)

	// Only test if the body is actually larger than limit
	if len(bodyBytes) <= MaxRequestBodySize {
		t.Skip("test body not large enough")
	}

	req := httptest.NewRequest("POST", "/api/ingest-thread", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleIngestThread(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestAgentServer_Close(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)

	// Should not panic
	err := server.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Calling close again should not panic
	err = server.Close()
	if err != nil {
		t.Errorf("Second Close() returned error: %v", err)
	}
}

func TestNewAgentServer(t *testing.T) {
	mockAgent := &mockAgentService{}
	cfg := &config.Config{}

	server := NewAgentServer(mockAgent, cfg)

	if server == nil {
		t.Fatal("NewAgentServer returned nil")
	}
	if server.agent == nil {
		t.Error("agent should not be nil")
	}
	if server.config == nil {
		t.Error("config should not be nil")
	}
	if server.mux == nil {
		t.Error("mux should not be nil")
	}

	server.Close()
}

func TestMaxRequestBodySize(t *testing.T) {
	// Verify the constant is 1MB
	expectedSize := int64(1 << 20) // 1MB
	if MaxRequestBodySize != expectedSize {
		t.Errorf("MaxRequestBodySize = %d, want %d", MaxRequestBodySize, expectedSize)
	}
}
