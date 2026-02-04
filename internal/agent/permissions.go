package agent

import (
	"context"
	"fmt"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
)

// MemoryPermissionChecker validates if a user/caller can save to memory
// based on email and groups from JWT
type MemoryPermissionChecker struct {
	config *config.PermissionsConfig

	// Pre-computed lookup maps for faster permission checks
	emailRoles map[string]string // email -> role ("write" or "read")
	groupRoles map[string]string // group -> role ("write" or "read")
}

// NewMemoryPermissionChecker creates a new permission checker
func NewMemoryPermissionChecker(cfg *config.PermissionsConfig) *MemoryPermissionChecker {
	checker := &MemoryPermissionChecker{
		config:     cfg,
		emailRoles: make(map[string]string),
		groupRoles: make(map[string]string),
	}

	// Build lookup maps from configuration
	for _, entry := range cfg.AllowedEmails {
		role := entry.Role
		if role == "" {
			role = ctxutil.RoleWrite // Default to write if not specified
		}
		checker.emailRoles[entry.Value] = role
	}

	for _, entry := range cfg.AllowedGroups {
		role := entry.Role
		if role == "" {
			role = ctxutil.RoleWrite // Default to write if not specified
		}
		checker.groupRoles[entry.Value] = role
	}

	return checker
}

// CanSaveToMemory checks if the current context has permission to save to memory
// Returns (allowed, reason) where reason explains why if denied
//
// Permission check order:
// 1. Role check: If role="read" in context, deny immediately (read-only access)
// 2. Email check: If user's email is in allowed_emails with role="write", allow
// 3. Group check: If user has any group in allowed_groups with role="write", allow
// 4. Default: Deny if permissions are configured, allow if empty
func (c *MemoryPermissionChecker) CanSaveToMemory(ctx context.Context) (bool, string) {
	log := logger.Get()
	callerID := ctxutil.CallerID(ctx)
	userEmail := ctxutil.UserEmail(ctx)
	userGroups := ctxutil.UserGroups(ctx)
	role := ctxutil.Role(ctx)

	log.Debugw("Permission check started",
		"caller_id", callerID,
		"user_email", userEmail,
		"user_groups", userGroups,
		"role", role,
		"config_empty", c.IsEmpty(),
		"allowed_emails_count", len(c.config.AllowedEmails),
		"allowed_groups_count", len(c.config.AllowedGroups),
	)

	// Check 1: Role-based access (from API key configuration or JWT)
	// If role is "read", the caller cannot write regardless of other permissions
	if role == ctxutil.RoleRead {
		reason := fmt.Sprintf("caller has role='read' (read-only access)")
		log.Infow("Permission denied: read-only role",
			"caller_id", callerID,
			"user_email", userEmail,
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

	// Check 2: Email-based permissions
	if userEmail != "" {
		if emailRole, found := c.emailRoles[userEmail]; found {
			if emailRole == ctxutil.RoleWrite {
				reason := fmt.Sprintf("email '%s' has write permission", userEmail)
				log.Infow("Permission granted: email allowed",
					"caller_id", callerID,
					"user_email", userEmail,
					"decision", "allowed",
					"reason", "allowed_email",
				)
				return true, reason
			}
			// Email found but with read-only role
			reason := fmt.Sprintf("email '%s' has read-only permission", userEmail)
			log.Infow("Permission denied: email has read-only permission",
				"caller_id", callerID,
				"user_email", userEmail,
				"decision", "denied",
				"reason", "email_read_only",
			)
			return false, reason
		}
	}

	// Check 3: Group-based permissions (check for any write-enabled group)
	if len(userGroups) > 0 {
		for _, group := range userGroups {
			if groupRole, found := c.groupRoles[group]; found {
				if groupRole == ctxutil.RoleWrite {
					reason := fmt.Sprintf("group '%s' has write permission", group)
					log.Infow("Permission granted: group allowed",
						"caller_id", callerID,
						"user_email", userEmail,
						"matched_group", group,
						"decision", "allowed",
						"reason", "allowed_group",
					)
					return true, reason
				}
			}
		}

		// Check if any group has read-only access (to provide a better error message)
		for _, group := range userGroups {
			if groupRole, found := c.groupRoles[group]; found && groupRole == ctxutil.RoleRead {
				reason := fmt.Sprintf("group '%s' has read-only permission", group)
				log.Infow("Permission denied: matching group has read-only permission",
					"caller_id", callerID,
					"user_email", userEmail,
					"matched_group", group,
					"decision", "denied",
					"reason", "group_read_only",
				)
				return false, reason
			}
		}
	}

	// No matching permissions found
	reason := "no matching email or group found in allowed permissions"
	if userEmail == "" && len(userGroups) == 0 {
		reason = "no user identity available (missing email and groups from JWT)"
	}

	log.Infow("Permission denied: no matching permissions",
		"caller_id", callerID,
		"user_email", userEmail,
		"user_groups", userGroups,
		"decision", "denied",
		"reason", "no_match",
	)
	return false, reason
}

// CanRead checks if the current context has permission to read from memory
// Read permission is granted if the user has either read or write permission
func (c *MemoryPermissionChecker) CanRead(ctx context.Context) (bool, string) {
	// If no permissions are configured, allow all reads
	if c.IsEmpty() {
		return true, "no permission restrictions configured"
	}

	userEmail := ctxutil.UserEmail(ctx)
	userGroups := ctxutil.UserGroups(ctx)

	// Check email permissions (any role allows read)
	if userEmail != "" {
		if _, found := c.emailRoles[userEmail]; found {
			return true, fmt.Sprintf("email '%s' has read permission", userEmail)
		}
	}

	// Check group permissions (any role allows read)
	for _, group := range userGroups {
		if _, found := c.groupRoles[group]; found {
			return true, fmt.Sprintf("group '%s' has read permission", group)
		}
	}

	return false, "no matching email or group found in allowed permissions"
}

// IsEmpty returns true if no permissions are configured
// When empty and role="write", the system allows all saves
func (c *MemoryPermissionChecker) IsEmpty() bool {
	return len(c.config.AllowedEmails) == 0 && len(c.config.AllowedGroups) == 0
}

// GetGroupsClaimPath returns the configured JWT claim path for groups
func (c *MemoryPermissionChecker) GetGroupsClaimPath() string {
	if c.config.GroupsClaimPath == "" {
		return "groups"
	}
	return c.config.GroupsClaimPath
}
