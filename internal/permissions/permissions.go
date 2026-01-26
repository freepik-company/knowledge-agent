package permissions

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
func (c *MemoryPermissionChecker) CanSaveToMemory(ctx context.Context) (bool, string) {
	callerID := ctxutil.CallerID(ctx)
	slackUserID := ctxutil.SlackUserID(ctx)

	// Check if caller ID is in admin list
	for _, adminID := range c.config.AdminCallerIDs {
		if callerID == adminID {
			return true, fmt.Sprintf("caller_id '%s' is admin", callerID)
		}
	}

	// Check if Slack user is in allowed list
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
// When empty, the system denies all saves (restrictive by default)
func (c *MemoryPermissionChecker) IsEmpty() bool {
	return len(c.config.AllowedSlackUsers) == 0 && len(c.config.AdminCallerIDs) == 0
}
