package a2a

import (
	"context"
	"encoding/json"

	"github.com/a2aproject/a2a-go/a2aclient"

	"knowledge-agent/internal/auth/keycloak"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
)

// Standard headers for identity propagation to sub-agents
const (
	HeaderUserID        = "X-User-ID"       // User identity for Langfuse (email preferred)
	HeaderUserEmail     = "X-User-Email"    // User's email for Keycloak identity
	HeaderUserGroups    = "X-User-Groups"   // User's groups as JSON array (for permission checking)
	HeaderSlackUserID   = "X-Slack-User-Id" // Original Slack user ID
	HeaderCallerID      = "X-Caller-Id"     // Caller identifier (for logging/permissions)
	HeaderSessionID     = "X-Session-Id"    // Session ID for Langfuse trace correlation
	HeaderAuthorization = "Authorization" // JWT Bearer token from Keycloak
)

// IdentityInterceptor propagates user identity to A2A sub-agent requests
// It extracts identity information from context and adds it as headers/metadata
type IdentityInterceptor struct {
	a2aclient.PassthroughInterceptor
	keycloakClient *keycloak.Client // nil if Keycloak is disabled
	agentName      string           // For logging
}

// NewIdentityInterceptor creates a new identity interceptor
// keycloakClient can be nil if Keycloak integration is disabled
func NewIdentityInterceptor(agentName string, keycloakClient *keycloak.Client) *IdentityInterceptor {
	return &IdentityInterceptor{
		keycloakClient: keycloakClient,
		agentName:      agentName,
	}
}

// Before adds identity headers to outgoing A2A requests
func (ii *IdentityInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	log := logger.Get()

	// Initialize metadata map if nil
	if req.Meta == nil {
		req.Meta = make(a2aclient.CallMeta)
	}

	// Extract identity from context
	slackUserID := ctxutil.SlackUserID(ctx)
	userEmail := ctxutil.UserEmail(ctx)
	userGroups := ctxutil.UserGroups(ctx)
	callerID := ctxutil.CallerID(ctx)
	sessionID := ctxutil.SessionID(ctx)

	// Propagate Slack User ID
	if slackUserID != "" {
		req.Meta[HeaderSlackUserID] = []string{slackUserID}
	}

	// Propagate user email
	if userEmail != "" {
		req.Meta[HeaderUserEmail] = []string{userEmail}
		// Also send as X-User-ID for Langfuse compatibility (fc-logs-agent reads this)
		req.Meta[HeaderUserID] = []string{userEmail}
	}

	// Propagate user groups as JSON array (for permission checking in sub-agents)
	if len(userGroups) > 0 {
		if groupsJSON, err := json.Marshal(userGroups); err == nil {
			req.Meta[HeaderUserGroups] = []string{string(groupsJSON)}
		}
	}

	// Propagate caller ID
	if callerID != "" && callerID != "unknown" {
		req.Meta[HeaderCallerID] = []string{callerID}
	}

	// Propagate session ID (for Langfuse trace correlation in sub-agents)
	if sessionID != "" {
		req.Meta[HeaderSessionID] = []string{sessionID}
	}

	// Add Keycloak JWT if enabled
	if ii.keycloakClient != nil && ii.keycloakClient.IsEnabled() {
		token, extraHeaders, err := ii.keycloakClient.GetTokenWithUserClaim(ctx, userEmail)
		if err != nil {
			log.Warnw("Failed to get Keycloak token for A2A request",
				"agent", ii.agentName,
				"error", err,
			)
			// Continue without token - don't fail the request
		} else if token != "" {
			req.Meta[HeaderAuthorization] = []string{"Bearer " + token}

			// Add extra headers from Keycloak (e.g., custom user claim for fallback)
			for k, v := range extraHeaders {
				req.Meta[k] = []string{v}
			}

			log.Debugw("Keycloak token added to A2A request",
				"agent", ii.agentName,
				"has_user_email", userEmail != "",
				"extra_headers", len(extraHeaders),
			)
		}
	}

	log.Debugw("Identity propagated to A2A request",
		"agent", ii.agentName,
		"method", req.Method,
		"has_slack_user_id", slackUserID != "",
		"has_user_email", userEmail != "",
		"has_user_groups", len(userGroups) > 0,
		"user_groups_count", len(userGroups),
		"has_session_id", sessionID != "",
		"has_keycloak", ii.keycloakClient != nil && ii.keycloakClient.IsEnabled(),
	)

	return ctx, nil
}
