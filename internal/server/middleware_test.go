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
		w.Write([]byte(callerID))
	}))

	tests := []struct {
		name           string
		token          string
		wantStatus     int
		wantCallerID   string
	}{
		{
			name:         "valid internal token",
			token:        "test-internal-token",
			wantStatus:   http.StatusOK,
			wantCallerID: "slack-bridge",
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

			if tt.wantStatus == http.StatusOK && rec.Body.String() != tt.wantCallerID {
				t.Errorf("got callerID %q, want %q", rec.Body.String(), tt.wantCallerID)
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
		APIKeys: map[string]string{
			"client-1": "secret-key-1",
			"client-2": "secret-key-2",
		},
	}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerID := ctxutil.CallerID(r.Context())
		w.Write([]byte(callerID))
	}))

	tests := []struct {
		name         string
		apiKey       string
		wantStatus   int
		wantCallerID string
	}{
		{
			name:         "valid API key for client-1",
			apiKey:       "secret-key-1",
			wantStatus:   http.StatusOK,
			wantCallerID: "client-1",
		},
		{
			name:         "valid API key for client-2",
			apiKey:       "secret-key-2",
			wantStatus:   http.StatusOK,
			wantCallerID: "client-2",
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

			if tt.wantStatus == http.StatusOK && rec.Body.String() != tt.wantCallerID {
				t.Errorf("got callerID %q, want %q", rec.Body.String(), tt.wantCallerID)
			}
		})
	}
}

func TestAuthMiddleware_OpenMode(t *testing.T) {
	// No auth configured = open mode
	cfg := &config.Config{}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerID := ctxutil.CallerID(r.Context())
		w.Write([]byte(callerID))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "unauthenticated" {
		t.Errorf("got callerID %q, want %q", rec.Body.String(), "unauthenticated")
	}
}

func TestAuthMiddleware_PriorityOrder(t *testing.T) {
	// Both internal token and API key configured
	cfg := &config.Config{
		Auth: config.AuthConfig{
			InternalToken: "internal-token",
		},
		APIKeys: map[string]string{
			"client-1": "api-key-1",
		},
	}

	handler := AuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerID := ctxutil.CallerID(r.Context())
		w.Write([]byte(callerID))
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
	// Internal token should win
	if rec.Body.String() != "slack-bridge" {
		t.Errorf("got callerID %q, want %q", rec.Body.String(), "slack-bridge")
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
