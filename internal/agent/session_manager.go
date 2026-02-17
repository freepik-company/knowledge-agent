package agent

import (
	"context"
	"strings"

	sessionredis "github.com/achetronic/adk-utils-go/session/redis"
	"google.golang.org/adk/session"

	"knowledge-agent/internal/logger"
)

// SessionManager handles session lifecycle with get-or-create semantics.
// Instead of overwriting sessions on every request, it preserves existing
// sessions to maintain multi-turn conversation history.
type SessionManager struct {
	sessionService *sessionredis.RedisSessionService
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(sessionService *sessionredis.RedisSessionService) *SessionManager {
	return &SessionManager{
		sessionService: sessionService,
	}
}

// GetOrCreateResult holds the result of a GetOrCreate operation.
type GetOrCreateResult struct {
	Session session.Session
	IsNew   bool
}

// GetOrCreate retrieves an existing session or creates a new one.
// Returns the session and whether it was newly created.
func (sm *SessionManager) GetOrCreate(ctx context.Context, appName, userID, sessionID string) (*GetOrCreateResult, error) {
	log := logger.Get()

	// Try to get existing session first
	getResp, err := sm.sessionService.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err == nil {
		log.Debugw("Reusing existing session",
			"session_id", sessionID,
			"user_id", userID,
			"events_count", getResp.Session.Events().Len(),
		)
		return &GetOrCreateResult{
			Session: getResp.Session,
			IsNew:   false,
		}, nil
	}

	// Check if error is "session not found" (expected for new sessions)
	if !strings.Contains(err.Error(), "session not found") {
		log.Warnw("Unexpected error getting session, will create new",
			"error", err,
			"session_id", sessionID,
		)
	}

	// Session doesn't exist, create a new one
	createResp, err := sm.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}

	log.Infow("Created new session",
		"session_id", sessionID,
		"user_id", userID,
	)

	return &GetOrCreateResult{
		Session: createResp.Session,
		IsNew:   true,
	}, nil
}
