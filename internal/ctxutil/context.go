package ctxutil

import "context"

type contextKey string

const (
	CallerIDKey    contextKey = "caller_id"
	SlackUserIDKey contextKey = "slack_user_id"
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
