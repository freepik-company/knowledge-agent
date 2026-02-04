package ctxutil

import "context"

type contextKey string

const (
	CallerIDKey    contextKey = "caller_id"
	SlackUserIDKey contextKey = "slack_user_id"
	RoleKey        contextKey = "role"
	UserEmailKey   contextKey = "user_email"  // User's email from Slack or JWT
	SessionIDKey   contextKey = "session_id"  // Session ID for Langfuse grouping and A2A propagation
	UserGroupsKey  contextKey = "user_groups" // User's groups from JWT (for permission checking)
)

// Role constants
const (
	RoleWrite = "write" // Can read and write to memory
	RoleRead  = "read"  // Can only read from memory (no save_to_memory)
)

// CallerID extracts the caller ID from the request context
func CallerID(ctx context.Context) string {
	if id, ok := ctx.Value(CallerIDKey).(string); ok {
		return id
	}
	return "unknown"
}

// SlackUserID extracts the Slack user ID from the request context
func SlackUserID(ctx context.Context) string {
	if id, ok := ctx.Value(SlackUserIDKey).(string); ok {
		return id
	}
	return ""
}

// Role extracts the role from the request context
// Returns "write" by default for backwards compatibility
func Role(ctx context.Context) string {
	if role, ok := ctx.Value(RoleKey).(string); ok {
		return role
	}
	return RoleWrite // Default to write for backwards compatibility
}

// CanWrite returns true if the context has write permissions
func CanWrite(ctx context.Context) bool {
	return Role(ctx) == RoleWrite
}

// UserEmail extracts the user's email from the request context
func UserEmail(ctx context.Context) string {
	if email, ok := ctx.Value(UserEmailKey).(string); ok {
		return email
	}
	return ""
}

// SessionID extracts the session ID from the request context
func SessionID(ctx context.Context) string {
	if id, ok := ctx.Value(SessionIDKey).(string); ok {
		return id
	}
	return ""
}

// UserGroups extracts the user's groups from the request context
func UserGroups(ctx context.Context) []string {
	if groups, ok := ctx.Value(UserGroupsKey).([]string); ok {
		return groups
	}
	return nil
}

// HasGroup checks if the user has a specific group
func HasGroup(ctx context.Context, group string) bool {
	groups := UserGroups(ctx)
	for _, g := range groups {
		if g == group {
			return true
		}
	}
	return false
}

// HasAnyGroup checks if the user has any of the specified groups
func HasAnyGroup(ctx context.Context, groups []string) bool {
	userGroups := UserGroups(ctx)
	if len(userGroups) == 0 {
		return false
	}
	userGroupSet := make(map[string]bool, len(userGroups))
	for _, g := range userGroups {
		userGroupSet[g] = true
	}
	for _, required := range groups {
		if userGroupSet[required] {
			return true
		}
	}
	return false
}
