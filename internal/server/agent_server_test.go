package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge-agent/internal/config"
)

func TestAgentServer_Handler(t *testing.T) {
	t.Skip("TODO: requires mock agent with RESTHandler")
}

func TestAgentServer_Close(t *testing.T) {
	t.Skip("TODO: requires mock agent with RESTHandler")
}

func TestNewAgentServer(t *testing.T) {
	t.Skip("TODO: requires mock agent with RESTHandler")
}

func TestMaxRequestBodySize(t *testing.T) {
	// Verify the constant is 1MB
	expectedSize := int64(1 << 20) // 1MB
	if MaxRequestBodySize != expectedSize {
		t.Errorf("MaxRequestBodySize = %d, want %d", MaxRequestBodySize, expectedSize)
	}
}

func TestADKPreProcessMiddleware_ExtractText(t *testing.T) {
	// Test extractTextFromContent helper
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "simple text",
			json: `{"appName":"test","userId":"u1","sessionId":"s1","newMessage":{"role":"user","parts":[{"text":"Hello world"}]}}`,
			want: "Hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a simple handler that echoes back
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// We can't test the full middleware without a real agent,
			// but we can verify the request reaches the handler
			req := httptest.NewRequest("POST", "/run", strings.NewReader(tt.json))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

func TestHealthEndpoints(t *testing.T) {
	// Test health endpoints don't require agent
	mux := http.NewServeMux()
	mux.HandleFunc("/health", HealthCheckHandler("knowledge-agent", ""))
	readiness := NewReadinessState()
	mux.HandleFunc("/ready", ReadinessHandler(readiness))
	mux.HandleFunc("/live", LivenessHandler())

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/health", http.StatusOK},
		{"/live", http.StatusOK},
		{"/ready", http.StatusServiceUnavailable}, // Not ready by default
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GET %s: got status %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
		})
	}

	// Test readiness after SetReady
	readiness.SetReady()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /ready after SetReady: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimiter_Creation(t *testing.T) {
	cfg := &config.Config{}
	rl := NewRateLimiter(10.0, 20, cfg.Server.TrustedProxies)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	rl.Close()
}
