package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/slack"
)

// jsonError sends an error response in JSON format
func jsonError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": message,
	})
}

// AuthMiddleware validates that the request comes from an authorized source
// It supports three authentication methods (checked in order):
// 1. Internal token via X-Internal-Token header (for Slack Bridge)
// 2. API Key via X-API-Key header (for external A2A access)
// 3. Slack signature via X-Slack-Signature header (legacy, for direct Slack webhooks)
//
// Authentication modes:
// - If internal_token is configured: Internal requests require X-Internal-Token
// - If a2a_api_keys is configured: External requests require X-API-Key
// - If neither is configured: Open mode (no authentication required)
func AuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.Get()

			// Option 1: Internal token (Slack Bridge → Agent)
			if cfg.Auth.InternalToken != "" {
				if token := r.Header.Get("X-Internal-Token"); token != "" {
					// Use constant-time comparison to prevent timing attacks
					if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Auth.InternalToken)) == 1 {
						ctx := context.WithValue(r.Context(), ctxutil.CallerIDKey, "slack-bridge")

						// Capture Slack user ID if provided
						if slackUserID := r.Header.Get("X-Slack-User-Id"); slackUserID != "" {
							ctx = context.WithValue(ctx, ctxutil.SlackUserIDKey, slackUserID)
						}

						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
					// Invalid internal token
					log.Warnw("Invalid internal token attempt",
						"path", r.URL.Path,
						"method", r.Method,
					)
					jsonError(w, "Invalid internal token", http.StatusUnauthorized)
					return
				}
			}

			// Option 2: API Key (external A2A access)
			if len(cfg.APIKeys) > 0 {
				if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
					// Search for client_id that has this secret
					for clientID, secret := range cfg.APIKeys {
						// Use constant-time comparison to prevent timing attacks
						if subtle.ConstantTimeCompare([]byte(secret), []byte(apiKey)) == 1 {
							ctx := context.WithValue(r.Context(), ctxutil.CallerIDKey, clientID)
							next.ServeHTTP(w, r.WithContext(ctx))
							return
						}
					}
					// Invalid API key
					log.Warnw("Invalid API key attempt",
						"path", r.URL.Path,
						"method", r.Method,
					)
					jsonError(w, "Invalid API key", http.StatusUnauthorized)
					return
				}
			}

			// Option 3: Slack signature (legacy, for direct Slack webhooks to agent)
			if r.Header.Get("X-Slack-Signature") != "" && cfg.Slack.SigningSecret != "" {
				if err := slack.VerifySlackRequest(r, cfg.Slack.SigningSecret); err == nil {
					ctx := context.WithValue(r.Context(), ctxutil.CallerIDKey, "slack-direct")
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				// Invalid Slack signature
				log.Warnw("Invalid Slack signature",
					"path", r.URL.Path,
					"method", r.Method,
				)
				jsonError(w, "Invalid Slack signature", http.StatusUnauthorized)
				return
			}

			// If no authentication methods configured → open mode
			if cfg.Auth.InternalToken == "" && len(cfg.APIKeys) == 0 {
				ctx := context.WithValue(r.Context(), ctxutil.CallerIDKey, "unauthenticated")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Authentication required but not provided
			log.Warnw("Authentication required but not provided",
				"path", r.URL.Path,
				"method", r.Method,
				"has_internal_token_config", cfg.Auth.InternalToken != "",
				"has_api_keys_config", len(cfg.APIKeys) > 0,
			)
			jsonError(w, "Authentication required", http.StatusUnauthorized)
		})
	}
}
