package a2a

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestExtractCallContext_Empty(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	cc := ExtractCallContext(req)

	if cc.RequestID == "" {
		t.Error("expected RequestID to be generated")
	}
	if len(cc.CallChain) != 0 {
		t.Errorf("expected empty CallChain, got %v", cc.CallChain)
	}
	if cc.CallDepth != 0 {
		t.Errorf("expected CallDepth 0, got %d", cc.CallDepth)
	}
}

func TestExtractCallContext_WithHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderRequestID, "test-request-123")
	req.Header.Set(HeaderCallChain, "agent-a,agent-b")
	req.Header.Set(HeaderCallDepth, "2")

	cc := ExtractCallContext(req)

	if cc.RequestID != "test-request-123" {
		t.Errorf("expected RequestID 'test-request-123', got '%s'", cc.RequestID)
	}
	if len(cc.CallChain) != 2 {
		t.Errorf("expected 2 agents in CallChain, got %d", len(cc.CallChain))
	}
	if cc.CallChain[0] != "agent-a" || cc.CallChain[1] != "agent-b" {
		t.Errorf("unexpected CallChain: %v", cc.CallChain)
	}
	if cc.CallDepth != 2 {
		t.Errorf("expected CallDepth 2, got %d", cc.CallDepth)
	}
}

func TestExtractCallContext_WhitespaceInChain(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderCallChain, " agent-a , agent-b ")

	cc := ExtractCallContext(req)

	if cc.CallChain[0] != "agent-a" || cc.CallChain[1] != "agent-b" {
		t.Errorf("expected whitespace to be trimmed, got: %v", cc.CallChain)
	}
}

func TestCallContext_ContainsAgent(t *testing.T) {
	cc := &CallContext{
		CallChain: []string{"agent-a", "agent-b"},
	}

	if !cc.ContainsAgent("agent-a") {
		t.Error("expected to find agent-a")
	}
	if !cc.ContainsAgent("AGENT-A") {
		t.Error("expected case-insensitive match for AGENT-A")
	}
	if !cc.ContainsAgent("agent-b") {
		t.Error("expected to find agent-b")
	}
	if cc.ContainsAgent("agent-c") {
		t.Error("did not expect to find agent-c")
	}
}

func TestCallContext_AddAgent(t *testing.T) {
	cc := &CallContext{
		RequestID: "test-123",
		CallChain: []string{"agent-a"},
		CallDepth: 1,
	}

	newCC := cc.AddAgent("agent-b")

	// Original should be unchanged
	if len(cc.CallChain) != 1 {
		t.Errorf("original CallChain should be unchanged, got %v", cc.CallChain)
	}
	if cc.CallDepth != 1 {
		t.Errorf("original CallDepth should be unchanged, got %d", cc.CallDepth)
	}

	// New context should have the added agent
	if len(newCC.CallChain) != 2 {
		t.Errorf("expected 2 agents, got %d", len(newCC.CallChain))
	}
	if newCC.CallChain[1] != "agent-b" {
		t.Errorf("expected agent-b to be added, got %v", newCC.CallChain)
	}
	if newCC.CallDepth != 2 {
		t.Errorf("expected CallDepth 2, got %d", newCC.CallDepth)
	}
	if newCC.RequestID != "test-123" {
		t.Errorf("expected RequestID to be preserved")
	}
}

func TestCallContext_SetHeaders(t *testing.T) {
	cc := &CallContext{
		RequestID: "test-123",
		CallChain: []string{"agent-a", "agent-b"},
		CallDepth: 2,
	}

	req := httptest.NewRequest("GET", "/test", nil)
	cc.SetHeaders(req)

	if req.Header.Get(HeaderRequestID) != "test-123" {
		t.Errorf("expected RequestID header 'test-123', got '%s'", req.Header.Get(HeaderRequestID))
	}
	if req.Header.Get(HeaderCallChain) != "agent-a,agent-b" {
		t.Errorf("expected CallChain header 'agent-a,agent-b', got '%s'", req.Header.Get(HeaderCallChain))
	}
	if req.Header.Get(HeaderCallDepth) != "2" {
		t.Errorf("expected CallDepth header '2', got '%s'", req.Header.Get(HeaderCallDepth))
	}
}

func TestContextFunctions(t *testing.T) {
	cc := &CallContext{
		RequestID: "test-456",
		CallChain: []string{"agent-x"},
		CallDepth: 1,
	}

	ctx := WithCallContext(context.Background(), cc)
	retrieved := GetCallContext(ctx)

	if retrieved.RequestID != cc.RequestID {
		t.Errorf("expected RequestID '%s', got '%s'", cc.RequestID, retrieved.RequestID)
	}
	if len(retrieved.CallChain) != len(cc.CallChain) {
		t.Errorf("expected CallChain length %d, got %d", len(cc.CallChain), len(retrieved.CallChain))
	}
	if retrieved.CallDepth != cc.CallDepth {
		t.Errorf("expected CallDepth %d, got %d", cc.CallDepth, retrieved.CallDepth)
	}
}

func TestGetCallContext_GeneratesRequestID(t *testing.T) {
	ctx := context.Background()
	cc := GetCallContext(ctx)

	if cc.RequestID == "" {
		t.Error("expected RequestID to be generated for empty context")
	}
}

func TestExtractCallContext_NegativeDepth(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderCallDepth, "-5")

	cc := ExtractCallContext(req)

	if cc.CallDepth != 0 {
		t.Errorf("negative depth should be treated as 0, got %d", cc.CallDepth)
	}
}

func TestExtractCallContext_InvalidDepth(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderCallDepth, "not-a-number")

	cc := ExtractCallContext(req)

	if cc.CallDepth != 0 {
		t.Errorf("invalid depth should be treated as 0, got %d", cc.CallDepth)
	}
}
