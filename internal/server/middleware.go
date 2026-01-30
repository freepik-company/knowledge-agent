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
// 1. Internal token via X-Internal-Token header (for Slack Bridge - TRUSTED)
// 2. API Key via X-API-Key header (for external A2A access - UNTRUSTED for user identity)
// 3. Slack signature via X-Slack-Signature header (legacy, for direct Slack webhooks)
//
// Authentication modes:
// - If internal_token is configured: Internal requests require X-Internal-Token
// - If api_keys is configured: External requests require X-API-Key
// - If neither is configured: Open mode (no authentication required)
//
// Security notes:
// - X-Slack-User-Id header is ONLY accepted from internal token auth (Slack Bridge)
// - External API key callers CANNOT spoof Slack user identity
// - Roles: "write" = read+write, "read" = read-only (no save_to_memory)
func AuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.Get()

			// Option 1: Internal token (Slack Bridge → Agent) - TRUSTED SOURCE
			// Only internal token auth can pass X-Slack-User-Id for user-level permissions
			if cfg.Auth.InternalToken != "" {
				if token := r.Header.Get("X-Internal-Token"); token != "" {
					// Use constant-time comparison to prevent timing attacks
					if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Auth.InternalToken)) == 1 {
						ctx := context.WithValue(r.Context(), ctxutil.CallerIDKey, "slack-bridge")
						ctx = context.WithValue(ctx, ctxutil.RoleKey, ctxutil.RoleWrite) // Internal always has write

						// ONLY accept Slack user ID from trusted internal source (Slack Bridge)
						// This prevents external agents from spoofing user identity
						if slackUserID := r.Header.Get("X-Slack-User-Id"); slackUserID != "" {
							ctx = context.WithValue(ctx, ctxutil.SlackUserIDKey, slackUserID)
						}

						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
					// Invalid internal token - don't fall through, reject immediately
					log.Warnw("Invalid internal token attempt",
						"path", r.URL.Path,
						"method", r.Method,
					)
					jsonError(w, "Invalid internal token", http.StatusUnauthorized)
					return
				}
				// No internal token provided - continue to check other auth methods
			}

			// Option 2: API Key (external A2A access) - UNTRUSTED for user identity
			// External callers authenticate as a service, NOT as a Slack user
			if len(cfg.APIKeys) > 0 {
				if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
					// Search for the API key configuration
					for secret, keyCfg := range cfg.APIKeys {
						// Use constant-time comparison to prevent timing attacks
						if subtle.ConstantTimeCompare([]byte(secret), []byte(apiKey)) == 1 {
							ctx := context.WithValue(r.Context(), ctxutil.CallerIDKey, keyCfg.CallerID)
							ctx = context.WithValue(ctx, ctxutil.RoleKey, keyCfg.Role)

							// SECURITY: Do NOT accept X-Slack-User-Id from external API keys
							// External agents authenticate as a service, not as a user
							// If they send X-Slack-User-Id, we ignore it to prevent spoofing
							if r.Header.Get("X-Slack-User-Id") != "" {
								log.Warnw("External API key attempted to pass Slack user ID (ignored)",
									"caller_id", keyCfg.CallerID,
									"attempted_slack_user_id", r.Header.Get("X-Slack-User-Id"),
									"path", r.URL.Path,
								)
							}

							log.Debugw("Authenticated via API key",
								"caller_id", keyCfg.CallerID,
								"role", keyCfg.Role,
								"path", r.URL.Path,
							)
							next.ServeHTTP(w, r.WithContext(ctx))
							return
						}
					}
					// Invalid API key - reject
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
					ctx = context.WithValue(ctx, ctxutil.RoleKey, ctxutil.RoleWrite) // Direct Slack has write
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
				ctx = context.WithValue(ctx, ctxutil.RoleKey, ctxutil.RoleWrite) // Open mode has write
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
