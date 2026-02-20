// +build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/server"
)

// TestUserNameIntegration verifies that user names are properly passed via ADK requests
func TestUserNameIntegration(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Skip("Skipping integration test: no config available")
	}

	ctx := context.Background()
	agnt, err := agent.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	defer agnt.Close()

	srv := server.NewAgentServer(agnt, cfg)

	t.Run("QueryWithUserContext", func(t *testing.T) {
		adkReq := `{
			"appName": "knowledge-agent",
			"userId": "test-user",
			"sessionId": "username-test-` + time.Now().Format("20060102150405") + `",
			"newMessage": {
				"role": "user",
				"parts": [{"text": "**User**: John Doe (@johndoe)\nHello, who am I?"}]
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/agent/run", strings.NewReader(adkReq))
		req.Header.Set("Content-Type", "application/json")
		if cfg.Auth.InternalToken != "" {
			req.Header.Set("X-Internal-Token", cfg.Auth.InternalToken)
		}

		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if body == "" {
			t.Error("Expected non-empty response body")
		}

		t.Logf("Response status: %d, body length: %d", w.Code, len(body))
	})
}
