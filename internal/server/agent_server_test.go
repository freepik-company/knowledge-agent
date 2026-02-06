package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/config"
)

// mockAgentService implements AgentInterface for testing
type mockAgentService struct {
	queryResponse *agent.QueryResponse
	queryError    error
	lastRequest   *agent.QueryRequest // Stores last request for verification
}

func (m *mockAgentService) QueryStream(ctx context.Context, req agent.QueryRequest, onEvent func(agent.StreamEvent)) error {
	m.lastRequest = &req
	if m.queryError != nil {
		onEvent(agent.StreamEvent{Type: "error", Message: m.queryError.Error()})
		return m.queryError
	}
	onEvent(agent.StreamEvent{Type: "start", MessageID: "test-msg-1"})
	onEvent(agent.StreamEvent{Type: "chunk", Content: "Test "})
	onEvent(agent.StreamEvent{Type: "chunk", Content: "answer"})
	onEvent(agent.StreamEvent{Type: "end", Status: "ok"})
	return nil
}

func (m *mockAgentService) Query(ctx context.Context, req agent.QueryRequest) (*agent.QueryResponse, error) {
	m.lastRequest = &req
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

func TestAgentServer_HandleQuery_WithIngestIntent(t *testing.T) {
	mockAgent := &mockAgentService{
		queryResponse: &agent.QueryResponse{
			Success: true,
			Answer:  "Thread ingested successfully",
		},
	}
	server := newTestServer(mockAgent, nil)
	defer server.Close()

	body := `{
		"question": "Ingest this thread",
		"intent": "ingest",
		"thread_ts": "1234567890.123456",
		"channel_id": "C123",
		"messages": [{"text": "hello", "user": "U123"}]
	}`
	req := httptest.NewRequest("POST", "/api/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d. body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response agent.QueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !response.Success {
		t.Error("expected success=true")
	}

	// Verify the intent was passed correctly
	if mockAgent.lastRequest == nil {
		t.Fatal("expected lastRequest to be set")
	}
	if mockAgent.lastRequest.Intent != "ingest" {
		t.Errorf("got intent %q, want %q", mockAgent.lastRequest.Intent, "ingest")
	}
	if mockAgent.lastRequest.ThreadTS != "1234567890.123456" {
		t.Errorf("got thread_ts %q, want %q", mockAgent.lastRequest.ThreadTS, "1234567890.123456")
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

// parseSSEEvents parses SSE data lines from response body
func parseSSEEvents(body string) ([]agent.StreamEvent, error) {
	var events []agent.StreamEvent
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var event agent.StreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, fmt.Errorf("failed to parse SSE event %q: %w", data, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func TestHandleQueryStream_Success(t *testing.T) {
	mockAgent := &mockAgentService{}
	server := newTestServer(mockAgent, nil)
	defer server.Close()

	body := `{"question": "What is the meaning of life?"}`
	req := httptest.NewRequest("POST", "/api/query/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleQueryStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify SSE headers
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
	if xab := rec.Header().Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("X-Accel-Buffering = %q, want %q", xab, "no")
	}

	// Parse SSE events
	events, err := parseSSEEvents(rec.Body.String())
	if err != nil {
		t.Fatalf("failed to parse SSE events: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}

	// Verify event sequence: start → chunk → chunk → end
	if events[0].Type != "start" {
		t.Errorf("event[0].Type = %q, want %q", events[0].Type, "start")
	}
	if events[0].MessageID != "test-msg-1" {
		t.Errorf("event[0].MessageID = %q, want %q", events[0].MessageID, "test-msg-1")
	}
	if events[1].Type != "chunk" || events[1].Content != "Test " {
		t.Errorf("event[1] = %+v, want chunk with 'Test '", events[1])
	}
	if events[2].Type != "chunk" || events[2].Content != "answer" {
		t.Errorf("event[2] = %+v, want chunk with 'answer'", events[2])
	}
	if events[3].Type != "end" || events[3].Status != "ok" {
		t.Errorf("event[3] = %+v, want end with status ok", events[3])
	}
}

func TestHandleQueryStream_MethodNotAllowed(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	req := httptest.NewRequest("GET", "/api/query/stream", nil)
	rec := httptest.NewRecorder()

	server.handleQueryStream(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleQueryStream_MissingQuestion(t *testing.T) {
	server := newTestServer(&mockAgentService{}, nil)
	defer server.Close()

	body := `{"channel_id": "C123"}`
	req := httptest.NewRequest("POST", "/api/query/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleQueryStream(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleQueryStream_Error(t *testing.T) {
	mockAgent := &mockAgentService{
		queryError: fmt.Errorf("agent failed"),
	}
	server := newTestServer(mockAgent, nil)
	defer server.Close()

	body := `{"question": "test"}`
	req := httptest.NewRequest("POST", "/api/query/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleQueryStream(rec, req)

	// SSE headers should still be set (response started before error)
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}

	events, err := parseSSEEvents(rec.Body.String())
	if err != nil {
		t.Fatalf("failed to parse SSE events: %v", err)
	}

	// Should have an error event
	hasError := false
	for _, e := range events {
		if e.Type == "error" && e.Message == "agent failed" {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Errorf("expected error event with message 'agent failed', got events: %+v", events)
	}
}
