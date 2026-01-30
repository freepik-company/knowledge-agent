package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/ctxutil"
)

func TestAuthMiddleware_InternalToken(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			InternalToken: "test-internal-token",
		},
	}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerID := ctxutil.CallerID(r.Context())
		role := ctxutil.Role(r.Context())
		w.Write([]byte(callerID + ":" + role))
	}))

	tests := []struct {
		name       string
		token      string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid internal token",
			token:      "test-internal-token",
			wantStatus: http.StatusOK,
			wantBody:   "slack-bridge:write",
		},
		{
			name:       "invalid internal token",
			token:      "wrong-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing token",
			token:      "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.token != "" {
				req.Header.Set("X-Internal-Token", tt.token)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK && rec.Body.String() != tt.wantBody {
				t.Errorf("got body %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestAuthMiddleware_InternalTokenWithSlackUserID(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			InternalToken: "test-internal-token",
		},
	}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackUserID := ctxutil.SlackUserID(r.Context())
		w.Write([]byte(slackUserID))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Slack-User-Id", "U1234567890")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "U1234567890" {
		t.Errorf("got slackUserID %q, want %q", rec.Body.String(), "U1234567890")
	}
}

func TestAuthMiddleware_APIKey(t *testing.T) {
	cfg := &config.Config{
		APIKeys: map[string]config.APIKeyConfig{
			"secret-key-1": {CallerID: "client-1", Role: "write"},
			"secret-key-2": {CallerID: "client-2", Role: "read"},
		},
	}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerID := ctxutil.CallerID(r.Context())
		role := ctxutil.Role(r.Context())
		w.Write([]byte(callerID + ":" + role))
	}))

	tests := []struct {
		name       string
		apiKey     string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid API key for client-1 (write role)",
			apiKey:     "secret-key-1",
			wantStatus: http.StatusOK,
			wantBody:   "client-1:write",
		},
		{
			name:       "valid API key for client-2 (read role)",
			apiKey:     "secret-key-2",
			wantStatus: http.StatusOK,
			wantBody:   "client-2:read",
		},
		{
			name:       "invalid API key",
			apiKey:     "wrong-key",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing API key",
			apiKey:     "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK && rec.Body.String() != tt.wantBody {
				t.Errorf("got body %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestAuthMiddleware_APIKeyWithSlackUserID(t *testing.T) {
	// SECURITY: External API keys should NOT be able to pass Slack User ID
	// This prevents external agents from spoofing Slack user identity
	cfg := &config.Config{
		APIKeys: map[string]config.APIKeyConfig{
			"secret-key": {CallerID: "external-agent", Role: "read"},
		},
	}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackUserID := ctxutil.SlackUserID(r.Context())
		callerID := ctxutil.CallerID(r.Context())
		role := ctxutil.Role(r.Context())
		w.Write([]byte(callerID + ":" + role + ":" + slackUserID))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("X-Slack-User-Id", "U1234567890") // This should be IGNORED for external API keys

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	// Slack User ID should be empty - external API keys cannot spoof user identity
	if rec.Body.String() != "external-agent:read:" {
		t.Errorf("got body %q, want %q (Slack User ID should be ignored for external API keys)", rec.Body.String(), "external-agent:read:")
	}
}

func TestAuthMiddleware_OpenMode(t *testing.T) {
	// No auth configured = open mode
	cfg := &config.Config{}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerID := ctxutil.CallerID(r.Context())
		role := ctxutil.Role(r.Context())
		w.Write([]byte(callerID + ":" + role))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "unauthenticated:write" {
		t.Errorf("got body %q, want %q", rec.Body.String(), "unauthenticated:write")
	}
}

func TestAuthMiddleware_PriorityOrder(t *testing.T) {
	// Both internal token and API key configured
	cfg := &config.Config{
		Auth: config.AuthConfig{
			InternalToken: "internal-token",
		},
		APIKeys: map[string]config.APIKeyConfig{
			"api-key-1": {CallerID: "client-1", Role: "read"},
		},
	}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerID := ctxutil.CallerID(r.Context())
		role := ctxutil.Role(r.Context())
		w.Write([]byte(callerID + ":" + role))
	}))

	// Test that internal token takes priority
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Internal-Token", "internal-token")
	req.Header.Set("X-API-Key", "api-key-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	// Internal token should win with write role
	if rec.Body.String() != "slack-bridge:write" {
		t.Errorf("got body %q, want %q", rec.Body.String(), "slack-bridge:write")
	}
}

func TestAuthMiddleware_ConstantTimeComparison(t *testing.T) {
	// This test verifies that the middleware doesn't leak timing information
	// We can't directly test constant-time behavior, but we verify the code path works
	cfg := &config.Config{
		Auth: config.AuthConfig{
			InternalToken: "correct-token-with-known-length",
		},
	}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Various tokens of different lengths
	tokens := []string{
		"x",
		"short",
		"correct-token-with-known-length",  // correct
		"correct-token-with-known-lengthx", // one char longer
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
	}

	for _, token := range tokens {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Internal-Token", token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		expectedStatus := http.StatusUnauthorized
		if token == "correct-token-with-known-length" {
			expectedStatus = http.StatusOK
		}

		if rec.Code != expectedStatus {
			t.Errorf("token %q: got status %d, want %d", token, rec.Code, expectedStatus)
		}
	}
}

func TestJsonError(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonError(rec, "test error message", http.StatusBadRequest)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusBadRequest)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("got Content-Type %q, want %q", contentType, "application/json")
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["success"] != false {
		t.Errorf("got success %v, want false", response["success"])
	}
	if response["message"] != "test error message" {
		t.Errorf("got message %q, want %q", response["message"], "test error message")
	}
}
