package agent

import (
	"context"
	"strings"

	sessionredis "github.com/achetronic/adk-utils-go/session/redis"
	"google.golang.org/adk/session"

	"knowledge-agent/internal/logger"
)

// isOrphanedToolCallError checks if the error is due to orphaned tool_use without tool_result
func isOrphanedToolCallError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "tool_use") &&
		strings.Contains(errStr, "tool_result") &&
		strings.Contains(errStr, "without")
}

// deleteCorruptedSession deletes a session that has orphaned tool calls
// This allows the next query to start fresh
func deleteCorruptedSession(ctx context.Context, sessionService *sessionredis.RedisSessionService, appName, userID, sessionID string) error {
	log := logger.Get()

	log.Warnw("Deleting corrupted session with orphaned tool calls",
		"app_name", appName,
		"user_id", userID,
		"session_id", sessionID,
	)

	err := sessionService.Delete(ctx, &session.DeleteRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})

	if err != nil {
		log.Errorw("Failed to delete corrupted session",
			"session_id", sessionID,
			"error", err,
		)
		return err
	}

	log.Infow("Corrupted session deleted, next query will start fresh",
		"session_id", sessionID,
	)

	return nil
}
