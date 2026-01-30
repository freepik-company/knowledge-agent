package agent

import (
	"context"
	"fmt"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/ctxutil"
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
	callerID := ctxutil.CallerID(ctx)
	slackUserID := ctxutil.SlackUserID(ctx)
	role := ctxutil.Role(ctx)

	// Check 1: Role-based access (from API key configuration)
	// If role is "read", the caller cannot write regardless of other permissions
	if role == ctxutil.RoleRead {
		return false, fmt.Sprintf("caller_id '%s' has role='read' (read-only access)", callerID)
	}

	// If no permissions are configured, allow writes for role="write" callers
	if c.IsEmpty() {
		return true, fmt.Sprintf("no permission restrictions configured, role='%s' allows write", role)
	}

	// Check 2: Admin caller IDs (full write access)
	for _, adminID := range c.config.AdminCallerIDs {
		if callerID == adminID {
			return true, fmt.Sprintf("caller_id '%s' is admin", callerID)
		}
	}

	// Check 3: Allowed Slack users
	if slackUserID != "" {
		for _, allowedUser := range c.config.AllowedSlackUsers {
			if slackUserID == allowedUser {
				return true, fmt.Sprintf("slack_user_id '%s' is allowed", slackUserID)
			}
		}

		// Slack user provided but not in allowed list
		return false, fmt.Sprintf("slack_user_id '%s' is not authorized to save to memory", slackUserID)
	}

	// No Slack user ID and caller not admin
	return false, fmt.Sprintf("caller_id '%s' is not authorized to save to memory", callerID)
}

// IsEmpty returns true if no permissions are configured
// When empty and role="write", the system allows all saves
func (c *MemoryPermissionChecker) IsEmpty() bool {
	return len(c.config.AllowedSlackUsers) == 0 && len(c.config.AdminCallerIDs) == 0
}
