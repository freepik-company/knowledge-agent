package ctxutil

import "context"

type contextKey string

const (
	CallerIDKey    contextKey = "caller_id"
	SlackUserIDKey contextKey = "slack_user_id"
	RoleKey        contextKey = "role"
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
