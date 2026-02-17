package agent

import (
	"context"
	"fmt"
	"strings"

	sessionredis "github.com/achetronic/adk-utils-go/session/redis"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"knowledge-agent/internal/logger"
)

// SessionSyncer synchronizes Slack thread messages as session events.
// It tracks the last synced timestamp to avoid re-syncing messages
// that have already been added to the session.
type SessionSyncer struct {
	sessionService *sessionredis.RedisSessionService
}

// NewSessionSyncer creates a new SessionSyncer.
func NewSessionSyncer(sessionService *sessionredis.RedisSessionService) *SessionSyncer {
	return &SessionSyncer{
		sessionService: sessionService,
	}
}

// messageGroup represents a group of consecutive messages with the same role.
type messageGroup struct {
	role string // "user" or "model"
	text string // merged text from all messages in the group
}

// SyncThreadMessages synchronizes Slack thread messages as session events.
// It only syncs messages that haven't been synced yet (based on last_synced_ts).
// The current message (last in the slice) is excluded since the runner will add it.
func (ss *SessionSyncer) SyncThreadMessages(ctx context.Context, sess session.Session, messages []map[string]any) error {
	log := logger.Get()

	if len(messages) == 0 {
		return nil
	}

	// Exclude the last message (the current one triggering this request)
	// The runner will add it automatically via runner.Run()
	messagesToSync := messages
	if len(messagesToSync) > 1 {
		messagesToSync = messagesToSync[:len(messagesToSync)-1]
	} else {
		// Only one message = the current one, nothing to sync
		return nil
	}

	// Get last synced timestamp from session state
	var lastSyncedTS string
	if val, err := sess.State().Get("last_synced_ts"); err == nil {
		if ts, ok := val.(string); ok {
			lastSyncedTS = ts
		}
	}

	// Filter messages: only those with ts > lastSyncedTS
	var newMessages []map[string]any
	for _, msg := range messagesToSync {
		ts := getStringFromMap(msg, "ts")
		if ts == "" {
			continue
		}
		if lastSyncedTS == "" || ts > lastSyncedTS {
			newMessages = append(newMessages, msg)
		}
	}

	if len(newMessages) == 0 {
		log.Debugw("No new messages to sync", "session_id", sess.ID(), "last_synced_ts", lastSyncedTS)
		return nil
	}

	// Group consecutive messages by role (merge same-role messages)
	groups := groupMessagesByRole(newMessages)

	// Create and append events for each group
	for _, group := range groups {
		event := session.NewEvent("")
		event.Content = &genai.Content{
			Role:  group.role,
			Parts: []*genai.Part{genai.NewPartFromText(group.text)},
		}
		event.Author = "slack-sync"

		if err := ss.sessionService.AppendEvent(ctx, sess, event); err != nil {
			return fmt.Errorf("failed to append synced event: %w", err)
		}
	}

	// Update last_synced_ts to the timestamp of the last synced message
	lastMsg := newMessages[len(newMessages)-1]
	if ts := getStringFromMap(lastMsg, "ts"); ts != "" {
		if err := sess.State().Set("last_synced_ts", ts); err != nil {
			log.Warnw("Failed to update last_synced_ts", "error", err)
		}
	}

	log.Infow("Synced thread messages to session",
		"session_id", sess.ID(),
		"new_messages", len(newMessages),
		"event_groups", len(groups),
		"last_synced_ts", lastSyncedTS,
	)

	return nil
}

// groupMessagesByRole groups consecutive messages by role, merging same-role messages.
// Bot messages (user starts with "bot:") become "model" role, others become "user" role.
func groupMessagesByRole(messages []map[string]any) []messageGroup {
	var groups []messageGroup

	for _, msg := range messages {
		user := getStringFromMap(msg, "user_name")
		if user == "" {
			user = getStringFromMap(msg, "user")
		}
		text := getStringFromMap(msg, "text")
		if text == "" {
			continue
		}

		// Determine role: bot messages have user starting with "bot:"
		role := "user"
		if strings.HasPrefix(user, "bot:") {
			role = "model"
		}

		// Format the message text
		var formattedText string
		if role == "user" {
			displayName := user
			if displayName == "" {
				displayName = "Unknown"
			}
			formattedText = fmt.Sprintf("[%s]: %s", displayName, text)
		} else {
			formattedText = text
		}

		// Merge with previous group if same role
		if len(groups) > 0 && groups[len(groups)-1].role == role {
			groups[len(groups)-1].text += "\n" + formattedText
		} else {
			groups = append(groups, messageGroup{
				role: role,
				text: formattedText,
			})
		}
	}

	return groups
}
