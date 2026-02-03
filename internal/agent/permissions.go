package agent

import (
	"context"
	"fmt"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
)

// MemoryPermissionChecker validates if a user/caller can save to memory
type MemoryPermissionChecker struct {
	config *config.PermissionsConfig
}

// NewMemoryPermissionChecker creates a new permission checker
func NewMemoryPermissionChecker(cfg *config.PermissionsConfig) *MemoryPermissionChecker {
	return &MemoryPermissionChecker{
		config: cfg,
	}
}

// CanSaveToMemory checks if the current context has permission to save to memory
// Returns (allowed, reason) where reason explains why if denied
//
// Permission check order:
// 1. Role check: If role="read", deny immediately (read-only agent)
// 2. Admin check: If caller_id is in admin_caller_ids, allow
// 3. Slack user check: If slack_user_id is in allowed_slack_users, allow
// 4. Default: Deny
func (c *MemoryPermissionChecker) CanSaveToMemory(ctx context.Context) (bool, string) {
	log := logger.Get()
	callerID := ctxutil.CallerID(ctx)
	slackUserID := ctxutil.SlackUserID(ctx)
	role := ctxutil.Role(ctx)

	log.Debugw("Permission check started",
		"caller_id", callerID,
		"slack_user_id", slackUserID,
		"role", role,
		"config_empty", c.IsEmpty(),
		"allowed_users_count", len(c.config.AllowedSlackUsers),
		"admin_ids_count", len(c.config.AdminCallerIDs),
	)

	// Check 1: Role-based access (from API key configuration)
	// If role is "read", the caller cannot write regardless of other permissions
	if role == ctxutil.RoleRead {
		reason := fmt.Sprintf("caller_id '%s' has role='read' (read-only access)", callerID)
		log.Infow("Permission denied: read-only role",
			"caller_id", callerID,
			"decision", "denied",
			"reason", "role_read",
		)
		return false, reason
	}

	// If no permissions are configured, allow writes for role="write" callers
	if c.IsEmpty() {
		reason := fmt.Sprintf("no permission restrictions configured, role='%s' allows write", role)
		log.Debugw("Permission granted: no restrictions configured",
			"caller_id", callerID,
			"decision", "allowed",
			"reason", "no_restrictions",
		)
		return true, reason
	}

	// Check 2: Admin caller IDs (full write access)
	for _, adminID := range c.config.AdminCallerIDs {
		if callerID == adminID {
			reason := fmt.Sprintf("caller_id '%s' is admin", callerID)
			log.Infow("Permission granted: admin caller",
				"caller_id", callerID,
				"decision", "allowed",
				"reason", "admin_caller",
			)
			return true, reason
		}
	}

	// Check 3: Allowed Slack users
	if slackUserID != "" {
		for _, allowedUser := range c.config.AllowedSlackUsers {
			if slackUserID == allowedUser {
				reason := fmt.Sprintf("slack_user_id '%s' is allowed", slackUserID)
				log.Infow("Permission granted: allowed Slack user",
					"caller_id", callerID,
					"slack_user_id", slackUserID,
					"decision", "allowed",
					"reason", "allowed_slack_user",
				)
				return true, reason
			}
		}

		// Slack user provided but not in allowed list
		reason := fmt.Sprintf("slack_user_id '%s' is not authorized to save to memory", slackUserID)
		log.Infow("Permission denied: Slack user not in allowed list",
			"caller_id", callerID,
			"slack_user_id", slackUserID,
			"decision", "denied",
			"reason", "slack_user_not_allowed",
		)
		return false, reason
	}

	// No Slack user ID and caller not admin
	reason := fmt.Sprintf("caller_id '%s' is not authorized to save to memory", callerID)
	log.Infow("Permission denied: caller not authorized",
		"caller_id", callerID,
		"decision", "denied",
		"reason", "caller_not_authorized",
	)
	return false, reason
}

// IsEmpty returns true if no permissions are configured
// When empty and role="write", the system allows all saves
func (c *MemoryPermissionChecker) IsEmpty() bool {
	return len(c.config.AllowedSlackUsers) == 0 && len(c.config.AdminCallerIDs) == 0
}
