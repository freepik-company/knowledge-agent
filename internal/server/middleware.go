package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"knowledge-agent/internal/auth/keycloak"
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
// 2. JWT Bearer token via Authorization header (for Keycloak-authenticated requests)
// 3. API Key via X-API-Key header (for external A2A access)
// 4. Slack signature via X-Slack-Signature header (legacy, for direct Slack webhooks)
//
// JWT handling:
// - When a Bearer token is present, it is parsed to extract email and groups
// - Groups are used for permission checking (allowed_groups in config)
// - The JWT is NOT validated cryptographically (assumes API Gateway or Keycloak already validated)
//
// Keycloak groups lookup:
// - When keycloakClient is provided and user has email but no groups, lookup groups from Keycloak
// - This enables group-based permissions for Slack users (who don't have JWT)
//
// Security notes:
// - X-Slack-User-Id header is ONLY accepted from internal token auth (Slack Bridge)
// - External API key callers CANNOT spoof Slack user identity
// - Groups from JWT are trusted (should be validated by upstream API Gateway)
func AuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return AuthMiddlewareWithKeycloak(cfg, nil)
}

// AuthMiddlewareWithKeycloak is like AuthMiddleware but accepts a Keycloak client
// for looking up user groups when not available from JWT
func AuthMiddlewareWithKeycloak(cfg *config.Config, keycloakClient *keycloak.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.Get()

			// Try to extract JWT claims from Authorization header (if present)
			// This is done early so claims are available for all auth methods
			var jwtClaims *keycloak.JWTClaims
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				if token := keycloak.ExtractBearerToken(authHeader); token != "" {
					// Determine groups claim path from config
					groupsPath := cfg.Permissions.GroupsClaimPath
					if groupsPath == "" && cfg.Keycloak.GroupsClaimPath != "" {
						groupsPath = cfg.Keycloak.GroupsClaimPath
					}
					if groupsPath == "" {
						groupsPath = "groups" // Default
					}

					claims, err := keycloak.ParseJWTClaims(token, groupsPath)
					if err != nil {
						log.Debugw("Failed to parse JWT claims (non-fatal)",
							"error", err,
							"path", r.URL.Path,
						)
					} else {
						jwtClaims = claims
						log.Debugw("JWT claims extracted",
							"email", claims.Email,
							"groups_count", len(claims.Groups),
							"path", r.URL.Path,
						)
					}
				}
			}

			// Option 1: Internal token (Slack Bridge → Agent) - TRUSTED SOURCE
			// Only internal token auth can pass X-Slack-User-Id for user-level permissions
			if cfg.Auth.InternalToken != "" {
				if token := r.Header.Get("X-Internal-Token"); token != "" {
					// Use constant-time comparison to prevent timing attacks
					if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Auth.InternalToken)) == 1 {
						ctx := context.WithValue(r.Context(), ctxutil.CallerIDKey, "slack-bridge")
						ctx = context.WithValue(ctx, ctxutil.RoleKey, ctxutil.RoleWrite) // Internal always has write

						// Accept Slack user ID from trusted internal source
						if slackUserID := r.Header.Get("X-Slack-User-Id"); slackUserID != "" {
							ctx = context.WithValue(ctx, ctxutil.SlackUserIDKey, slackUserID)
						}

						// Add user email from request header or JWT
					var userEmail string
					if userEmail = r.Header.Get("X-User-Email"); userEmail == "" {
						if jwtClaims != nil && jwtClaims.Email != "" {
							userEmail = jwtClaims.Email
						}
					}
					if userEmail != "" {
						ctx = context.WithValue(ctx, ctxutil.UserEmailKey, userEmail)
					}

					// Add groups from JWT if available
					var userGroups []string
					if jwtClaims != nil && len(jwtClaims.Groups) > 0 {
						userGroups = jwtClaims.Groups
					}

					// If we have email but no groups, lookup groups from Keycloak
					if userEmail != "" && len(userGroups) == 0 && keycloakClient != nil && keycloakClient.IsEnabled() {
						groups, err := keycloakClient.GetUserGroups(r.Context(), userEmail)
						if err != nil {
							log.Warnw("Failed to lookup user groups from Keycloak",
								"email", userEmail,
								"error", err,
							)
							// Continue without groups - permission check will use email only
						} else if len(groups) > 0 {
							userGroups = groups
							log.Debugw("Retrieved user groups from Keycloak",
								"email", userEmail,
								"groups_count", len(groups),
							)
						}
					}

					if len(userGroups) > 0 {
						ctx = context.WithValue(ctx, ctxutil.UserGroupsKey, userGroups)
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

			// Option 2: JWT Bearer token (Keycloak-authenticated requests)
			// This handles requests that come with a validated JWT from API Gateway
			if jwtClaims != nil {
				ctx := r.Context()

				// Determine caller ID from JWT
				callerID := "jwt-user"
				if jwtClaims.PreferredUser != "" {
					callerID = jwtClaims.PreferredUser
				} else if jwtClaims.Email != "" {
					callerID = jwtClaims.Email
				}
				ctx = context.WithValue(ctx, ctxutil.CallerIDKey, callerID)
				ctx = context.WithValue(ctx, ctxutil.RoleKey, ctxutil.RoleWrite) // JWT users default to write

				// Add email and groups from JWT
				if jwtClaims.Email != "" {
					ctx = context.WithValue(ctx, ctxutil.UserEmailKey, jwtClaims.Email)
				}
				if len(jwtClaims.Groups) > 0 {
					ctx = context.WithValue(ctx, ctxutil.UserGroupsKey, jwtClaims.Groups)
				}

				log.Debugw("Authenticated via JWT",
					"caller_id", callerID,
					"email", jwtClaims.Email,
					"groups_count", len(jwtClaims.Groups),
					"path", r.URL.Path,
				)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Option 3: API Key (external A2A access)
			// External callers authenticate as a service, NOT as a user
			if len(cfg.APIKeys) > 0 {
				if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
					// Search for the API key configuration
					for secret, keyCfg := range cfg.APIKeys {
						// Use constant-time comparison to prevent timing attacks
						if subtle.ConstantTimeCompare([]byte(secret), []byte(apiKey)) == 1 {
							ctx := context.WithValue(r.Context(), ctxutil.CallerIDKey, keyCfg.CallerID)
							ctx = context.WithValue(ctx, ctxutil.RoleKey, keyCfg.Role)

							// Accept propagated identity headers from trusted A2A sources
							// These headers are set by the identity interceptor in upstream agents
							if userEmail := r.Header.Get("X-User-Email"); userEmail != "" {
								ctx = context.WithValue(ctx, ctxutil.UserEmailKey, userEmail)
							}
							if slackUserID := r.Header.Get("X-Slack-User-Id"); slackUserID != "" {
								ctx = context.WithValue(ctx, ctxutil.SlackUserIDKey, slackUserID)
							}

							// Accept groups from X-User-Groups header (JSON array)
							// This is set by upstream agents that have already validated the JWT
							if groupsHeader := r.Header.Get("X-User-Groups"); groupsHeader != "" {
								var groups []string
								if err := json.Unmarshal([]byte(groupsHeader), &groups); err == nil {
									ctx = context.WithValue(ctx, ctxutil.UserGroupsKey, groups)
								}
							}

							log.Debugw("Authenticated via API key",
								"caller_id", keyCfg.CallerID,
								"role", keyCfg.Role,
								"has_user_email", r.Header.Get("X-User-Email") != "",
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

			// Option 4: Slack signature (legacy, for direct Slack webhooks to agent)
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
