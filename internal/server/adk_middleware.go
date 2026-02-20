package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/observability"
)

// adkRunRequest mirrors the ADK RunAgentRequest structure for pre-processing.
type adkRunRequest struct {
	AppName    string         `json:"appName"`
	UserID     string         `json:"userId"`
	SessionID  string         `json:"sessionId"`
	NewMessage genai.Content  `json:"newMessage"`
	Streaming  bool           `json:"streaming,omitempty"`
	StateDelta map[string]any `json:"stateDelta,omitempty"`
}

// ADKPreProcessMiddleware wraps the ADK REST handler with pre-processing logic:
// - Creates Langfuse trace
// - Executes pre-search memory and injects results into the user message
// - Manages session compaction
// - Sets up context for permission checks
func ADKPreProcessMiddleware(ag *agent.Agent) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.Get()

			// Only pre-process /run and /run_sse (agent execution endpoints).
			// Pass through session CRUD and other ADK endpoints unchanged.
			if r.Method != http.MethodPost || (r.URL.Path != "/run" && r.URL.Path != "/run_sse") {
				next.ServeHTTP(w, r)
				return
			}

			// Read and parse the ADK request body
			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBodySize))
			r.Body.Close()
			if err != nil {
				http.Error(w, "Failed to read request body", http.StatusBadRequest)
				return
			}

			var adkReq adkRunRequest
			if err := json.Unmarshal(bodyBytes, &adkReq); err != nil {
				// Not a valid ADK request - pass through (might be OPTIONS, etc.)
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			callerID := ctxutil.CallerID(ctx)
			slackUserID := ctxutil.SlackUserID(ctx)

			// Extract user's text from the message
			userText := extractTextFromContent(&adkReq.NewMessage)

			// Resolve session ID: use provided or generate from context
			sessionID := adkReq.SessionID
			if sessionID == "" {
				sessionID = agent.ResolveSessionID("", "", "")
			}

			// Resolve user ID for memory operations.
			// When knowledge_scope is "shared", always use "shared-knowledge"
			// regardless of client-provided userId (e.g., Agentgram sends "agentgram").
			// For other scopes, use the client-provided userId or resolve from context.
			var userID string
			scope := ag.GetConfig().RAG.KnowledgeScope
			if scope == "shared" {
				userID = agent.ResolveUserID(scope, "", "")
			} else if adkReq.UserID != "" {
				userID = adkReq.UserID
			} else {
				userID = agent.ResolveUserID(scope, "", slackUserID)
			}

			// Start Langfuse trace
			traceMetadata := map[string]any{
				"caller_id":     callerID,
				"slack_user_id": slackUserID,
				"session_id":    sessionID,
				"user_id":       userID,
				"user_email":    ctxutil.UserEmail(ctx),
			}
			trace := ag.GetLangfuseTracer().StartQueryTrace(ctx, userText, sessionID, traceMetadata)
			ctx = observability.ContextWithQueryTrace(ctx, trace)

			// Log permission info
			canSave, permissionReason := ag.GetPermissionChecker().CanSaveToMemory(ctx)
			if !ag.GetPermissionChecker().IsEmpty() {
				log.Infow("Processing ADK request",
					"caller_id", callerID,
					"session_id", sessionID,
					"can_save_to_memory", canSave,
					"permission_reason", permissionReason,
				)
			} else {
				log.Infow("Processing ADK request",
					"caller_id", callerID,
					"session_id", sessionID,
				)
			}

			// Get or create session
			_, err = ag.GetSessionManager().GetOrCreate(ctx, agent.AppName, userID, sessionID)
			if err != nil {
				log.Errorw("Failed to get or create session", "error", err, "session_id", sessionID)
				trace.End(false, fmt.Sprintf("Session error: %v", err))
				http.Error(w, "Session error", http.StatusInternalServerError)
				return
			}

			// Compact session proactively
			if err := ag.GetSessionCompactor().CompactIfNeeded(ctx, agent.AppName, userID, sessionID); err != nil {
				log.Warnw("Session compaction failed", "error", err, "session_id", sessionID)
			}

			// Pre-search memory and inject results into the user message
			preSearchResults := ag.PreSearchMemory(ctx, userText, userID)
			if preSearchResults != "" {
				adkReq.NewMessage = injectPreSearchIntoMessage(&adkReq.NewMessage, preSearchResults)
			}

			// Inject date context into the user message
			currentDate := time.Now().Format("Monday, January 2, 2006")
			adkReq.NewMessage = injectDateContext(&adkReq.NewMessage, currentDate, ctxutil.UserEmail(ctx))

			// Ensure session ID and user ID are set
			if adkReq.SessionID == "" {
				adkReq.SessionID = sessionID
			}
			if adkReq.UserID == "" {
				adkReq.UserID = userID
			}
			if adkReq.AppName == "" {
				adkReq.AppName = agent.AppName
			}

			// Add identity to context for sub-agent propagation
			if email := ctxutil.UserEmail(ctx); email != "" {
				ctx = context.WithValue(ctx, ctxutil.UserEmailKey, email)
			}
			ctx = context.WithValue(ctx, ctxutil.SessionIDKey, sessionID)

			// Wrap response writer to capture status code for trace outcome
			sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Record metrics and close trace on completion
			startTime := time.Now()
			defer func() {
				success := sw.statusCode >= 200 && sw.statusCode < 400
				observability.GetMetrics().RecordQuery(time.Since(startTime), nil)
				agent.LogTraceSummary(trace, ag.GetConfig().Anthropic.Model,
					ag.GetConfig().Langfuse.InputCostPer1M,
					ag.GetConfig().Langfuse.OutputCostPer1M)
				trace.End(success, "")
			}()

			// Reconstruct request body with modified message
			modifiedBody, err := json.Marshal(adkReq)
			if err != nil {
				log.Errorw("Failed to marshal modified ADK request", "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return // defer will call trace.End()
			}

			// Replace request body and update content length
			r.Body = io.NopCloser(bytes.NewReader(modifiedBody))
			r.ContentLength = int64(len(modifiedBody))

			// Pass the enriched context to the ADK handler
			next.ServeHTTP(sw, r.WithContext(ctx))
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.written {
		sw.statusCode = code
		sw.written = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.written {
		sw.statusCode = http.StatusOK
		sw.written = true
	}
	return sw.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter, allowing http.ResponseController
// to access optional interfaces (Flusher, SetWriteDeadline, etc.) needed by ADK SSE.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}

// extractTextFromContent extracts the text from a genai.Content message.
func extractTextFromContent(content *genai.Content) string {
	var texts []string
	for _, part := range content.Parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, " ")
}

// injectPreSearchIntoMessage prepends pre-search results to the user's message text.
func injectPreSearchIntoMessage(msg *genai.Content, preSearchResults string) genai.Content {
	result := genai.Content{
		Role:  msg.Role,
		Parts: make([]*genai.Part, 0, len(msg.Parts)+1),
	}

	// Prepend pre-search results as a text part
	preSearchPart := genai.NewPartFromText(fmt.Sprintf("**Memory** (pre-searched):\n%s\n", preSearchResults))
	result.Parts = append(result.Parts, preSearchPart)

	// Copy original parts
	result.Parts = append(result.Parts, msg.Parts...)
	return result
}

// injectDateContext prepends date and user context to the user's message.
func injectDateContext(msg *genai.Content, currentDate, userEmail string) genai.Content {
	result := genai.Content{
		Role:  msg.Role,
		Parts: make([]*genai.Part, 0, len(msg.Parts)+1),
	}

	// Build context header
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Date**: %s", currentDate)

	if userEmail != "" {
		fmt.Fprintf(&sb, "\n**User**: %s", userEmail)
	}

	contextPart := genai.NewPartFromText(sb.String())

	// Prepend context before all existing parts
	result.Parts = append(result.Parts, contextPart)
	result.Parts = append(result.Parts, msg.Parts...)
	return result
}
