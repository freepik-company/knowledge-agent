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
	// Initialize logger for tests
	logger.Initialize(logger.Config{Level: "error", Format: "console"})
}

func TestLoopPreventionMiddleware_Disabled(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled: false,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := LoopPreventionMiddleware(cfg)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderCallChain, "some-agent")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 when disabled, got %d", rr.Code)
	}
}

func TestLoopPreventionMiddleware_NoLoop(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:      true,
		SelfName:     "my-agent",
		MaxCallDepth: 5,
	}

	contextChecked := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify call context is set
		cc := GetCallContext(r.Context())
		if !cc.ContainsAgent("my-agent") {
			t.Error("expected my-agent to be in call chain")
		}
		contextChecked = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := LoopPreventionMiddleware(cfg)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderCallChain, "other-agent")
	req.Header.Set(HeaderCallDepth, "1")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if !contextChecked {
		t.Error("handler was not called")
	}
}

func TestLoopPreventionMiddleware_LoopDetected(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:      true,
		SelfName:     "my-agent",
		MaxCallDepth: 5,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when loop is detected")
	})

	middleware := LoopPreventionMiddleware(cfg)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderCallChain, "other-agent,my-agent") // my-agent already in chain
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != StatusLoopDetected {
		t.Errorf("expected status %d, got %d", StatusLoopDetected, rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["success"] != false {
		t.Error("expected success to be false")
	}
	if resp["error"] == nil {
		t.Error("expected error message")
	}
}

func TestLoopPreventionMiddleware_MaxDepthExceeded(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:      true,
		SelfName:     "my-agent",
		MaxCallDepth: 3,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when max depth is exceeded")
	})

	middleware := LoopPreventionMiddleware(cfg)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderCallChain, "a,b,c")
	req.Header.Set(HeaderCallDepth, "3") // Equal to max, should be rejected
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != StatusLoopDetected {
		t.Errorf("expected status %d, got %d", StatusLoopDetected, rr.Code)
	}
}

func TestLoopPreventionMiddleware_CaseInsensitiveLoop(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:      true,
		SelfName:     "My-Agent",
		MaxCallDepth: 5,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when loop is detected")
	})

	middleware := LoopPreventionMiddleware(cfg)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderCallChain, "my-agent") // lowercase version
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != StatusLoopDetected {
		t.Errorf("expected status %d for case-insensitive match, got %d", StatusLoopDetected, rr.Code)
	}
}

func TestLoopPreventionMiddleware_EmptySelfName(t *testing.T) {
	cfg := &config.A2AConfig{
		Enabled:      true,
		SelfName:     "", // Empty self name
		MaxCallDepth: 5,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := LoopPreventionMiddleware(cfg)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	// Should pass through when self_name is empty
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 when self_name is empty, got %d", rr.Code)
	}
}
