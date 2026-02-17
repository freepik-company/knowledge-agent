package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	sessionredis "github.com/achetronic/adk-utils-go/session/redis"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// DefaultCompactPrompt is the default prompt for session compaction.
const DefaultCompactPrompt = `Summarize this conversation history concisely while preserving critical information.

PRESERVE (keep exactly as written):
- Decisions and conclusions reached
- Technical details: configs, IPs, ports, service names, versions
- Error messages and their resolutions
- Numerical data, metrics, and statistics
- Code snippets, commands, and file paths
- Names, dates, and deadlines mentioned
- Action items and commitments
- Key questions asked and answers given

REMOVE:
- Repetitive greetings and pleasantries
- Redundant back-and-forth exchanges
- Filler text and conversational padding
- Duplicate information

IMPORTANT:
- Output ONLY the summary, no explanations or meta-commentary
- Maintain the original language (Spanish, English, etc.)
- Keep the chronological flow of events
- Use bullet points for clarity

Conversation to summarize:
%s`

// SessionCompactor handles compaction of session events when the context grows too large.
type SessionCompactor struct {
	client         anthropic.Client
	model          string
	keepTurns      int
	threshold      int
	sessionService *sessionredis.RedisSessionService
	enabled        bool
}

// NewSessionCompactor creates a new SessionCompactor.
func NewSessionCompactor(cfg *config.Config, sessionService *sessionredis.RedisSessionService) *SessionCompactor {
	model := cfg.Session.CompactModel
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	keepTurns := cfg.Session.CompactKeepTurns
	if keepTurns <= 0 {
		keepTurns = 4
	}

	threshold := cfg.Session.CompactThreshold
	if threshold <= 0 {
		threshold = 8000
	}

	// Need API key to create compaction client
	if cfg.Anthropic.APIKey == "" {
		return &SessionCompactor{enabled: false, sessionService: sessionService}
	}

	client := anthropic.NewClient(option.WithAPIKey(cfg.Anthropic.APIKey))

	return &SessionCompactor{
		client:         client,
		model:          model,
		keepTurns:      keepTurns,
		threshold:      threshold,
		sessionService: sessionService,
		enabled:        true,
	}
}

// estimateTokens provides a rough estimate of token count (~4 chars per token).
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text) / 4
}

// CompactIfNeeded checks if the session needs compaction and performs it if so.
func (sc *SessionCompactor) CompactIfNeeded(ctx context.Context, appName, userID, sessionID string) error {
	if !sc.enabled {
		return nil
	}

	log := logger.Get()

	// Get session to check event count and size
	getResp, err := sc.sessionService.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("failed to get session for compaction check: %w", err)
	}

	sess := getResp.Session
	events := collectEvents(sess)

	if len(events) == 0 {
		return nil
	}

	// Estimate total tokens from all events
	totalText := buildTextFromEvents(events)
	estimatedTokens := estimateTokens(totalText)

	if estimatedTokens < sc.threshold {
		log.Debugw("Session below compaction threshold",
			"session_id", sessionID,
			"estimated_tokens", estimatedTokens,
			"threshold", sc.threshold,
			"events_count", len(events),
		)
		return nil
	}

	log.Infow("Session exceeds compaction threshold, compacting",
		"session_id", sessionID,
		"estimated_tokens", estimatedTokens,
		"threshold", sc.threshold,
		"events_count", len(events),
	)

	return sc.doCompact(ctx, appName, userID, sessionID, sess, events)
}

// Compact forces compaction regardless of threshold (used for overflow recovery).
func (sc *SessionCompactor) Compact(ctx context.Context, appName, userID, sessionID string) error {
	if !sc.enabled {
		return nil
	}

	log := logger.Get()

	getResp, err := sc.sessionService.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("failed to get session for forced compaction: %w", err)
	}

	sess := getResp.Session
	events := collectEvents(sess)

	if len(events) == 0 {
		return nil
	}

	log.Infow("Forcing session compaction",
		"session_id", sessionID,
		"events_count", len(events),
	)

	return sc.doCompact(ctx, appName, userID, sessionID, sess, events)
}

// doCompact performs the actual compaction:
// 1. Separate events into "old" and "recent" (keep last keepTurns*2 events)
// 2. Summarize old events with Haiku
// 3. Delete old session
// 4. Create new session with summary + recent events
func (sc *SessionCompactor) doCompact(ctx context.Context, appName, userID, sessionID string, sess session.Session, events []*session.Event) error {
	log := logger.Get()

	// Determine split point: keep last keepTurns*2 events as "recent"
	recentCount := sc.keepTurns * 2
	if recentCount > len(events) {
		recentCount = len(events)
	}

	splitIdx := len(events) - recentCount
	oldEvents := events[:splitIdx]
	recentEvents := events[splitIdx:]

	if len(oldEvents) == 0 {
		log.Debugw("No old events to compact, all events are recent",
			"session_id", sessionID,
			"total_events", len(events),
		)
		return nil
	}

	// Build text from old events for summarization
	oldText := buildTextFromEvents(oldEvents)

	// Summarize old events using Haiku
	summary, err := sc.summarize(ctx, oldText)
	if err != nil {
		return fmt.Errorf("failed to summarize old events: %w", err)
	}

	// Preserve last_synced_ts from session state
	var lastSyncedTS string
	if val, stateErr := sess.State().Get("last_synced_ts"); stateErr == nil {
		if ts, ok := val.(string); ok {
			lastSyncedTS = ts
		}
	}

	// Delete old session
	if err := sc.sessionService.Delete(ctx, &session.DeleteRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	}); err != nil {
		return fmt.Errorf("failed to delete old session: %w", err)
	}

	// Create new session with preserved state
	state := map[string]any{}
	if lastSyncedTS != "" {
		state["last_synced_ts"] = lastSyncedTS
	}

	createResp, err := sc.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
		State:     state,
	})
	if err != nil {
		return fmt.Errorf("failed to create new session after compaction: %w", err)
	}

	newSess := createResp.Session

	// Add summary as first event pair (user + model)
	summaryUserEvent := session.NewEvent("")
	summaryUserEvent.Content = &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{genai.NewPartFromText(fmt.Sprintf("[Conversation Summary]\n%s", summary))},
	}
	summaryUserEvent.Author = "compactor"

	if err := sc.sessionService.AppendEvent(ctx, newSess, summaryUserEvent); err != nil {
		return fmt.Errorf("failed to append summary user event: %w", err)
	}

	summaryModelEvent := session.NewEvent("")
	summaryModelEvent.Content = &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{genai.NewPartFromText("Understood, I have the context from the conversation summary.")},
	}
	summaryModelEvent.Author = "compactor"

	if err := sc.sessionService.AppendEvent(ctx, newSess, summaryModelEvent); err != nil {
		return fmt.Errorf("failed to append summary model event: %w", err)
	}

	// Re-add all recent events
	for _, evt := range recentEvents {
		if err := sc.sessionService.AppendEvent(ctx, newSess, evt); err != nil {
			return fmt.Errorf("failed to re-add recent event: %w", err)
		}
	}

	log.Infow("Session compacted successfully",
		"session_id", sessionID,
		"old_events", len(oldEvents),
		"recent_events", len(recentEvents),
		"summary_length", len(summary),
		"last_synced_ts_preserved", lastSyncedTS != "",
	)

	return nil
}

// summarize calls Haiku to create a summary of the old events.
func (sc *SessionCompactor) summarize(ctx context.Context, text string) (string, error) {
	prompt := fmt.Sprintf(DefaultCompactPrompt, text)

	summarizeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	message, err := sc.client.Messages.New(summarizeCtx, anthropic.MessageNewParams{
		Model:     anthropic.Model(sc.model),
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("haiku summarization failed: %w", err)
	}

	var result strings.Builder
	for _, block := range message.Content {
		if block.Type == "text" {
			result.WriteString(block.Text)
		}
	}

	if result.Len() == 0 {
		return "", fmt.Errorf("summarization returned empty response")
	}

	return result.String(), nil
}

// collectEvents extracts all events from a session into a slice.
func collectEvents(sess session.Session) []*session.Event {
	var events []*session.Event
	for evt := range sess.Events().All() {
		events = append(events, evt)
	}
	return events
}

// buildTextFromEvents builds a text representation of events for token estimation and summarization.
func buildTextFromEvents(events []*session.Event) string {
	var sb strings.Builder
	for _, evt := range events {
		if evt.Content != nil {
			role := evt.Content.Role
			for _, part := range evt.Content.Parts {
				if part.Text != "" {
					fmt.Fprintf(&sb, "[%s]: %s\n", role, part.Text)
				}
			}
		}
	}
	return sb.String()
}

// isContextOverflowError checks if the error indicates the context/prompt is too long.
func isContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "prompt is too long") ||
		strings.Contains(errStr, "too many tokens") ||
		strings.Contains(errStr, "maximum context length") ||
		strings.Contains(errStr, "content too large")
}
