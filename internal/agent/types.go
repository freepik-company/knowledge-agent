package agent

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// Import types needed for agent functionality
// This allows us to use them without importing directly in other files
type (
	RunConfig     = agent.RunConfig
	GetRequest    = session.GetRequest
	CreateRequest = session.CreateRequest
)

var (
	// Re-export genai functions for convenience
	NewContentFromText = genai.NewContentFromText
	RoleUser           = genai.RoleUser
)

// QueryRequest represents a query request
type QueryRequest struct {
	ConversationID string           `json:"conversation_id,omitempty"` // Optional: client-provided conversation ID (takes precedence over auto-generated)
	Query          string           `json:"query"`
	Intent         string           `json:"intent,omitempty"` // "query" (default) or "ingest" - determines behavior
	ThreadTS       string           `json:"thread_ts,omitempty"`
	ChannelID      string           `json:"channel_id,omitempty"`
	Messages       []map[string]any `json:"messages,omitempty"`       // Current thread context
	UserName       string           `json:"user_name,omitempty"`      // Slack @username
	UserRealName   string           `json:"user_real_name,omitempty"` // User's real name
	UserEmail      string           `json:"user_email,omitempty"`     // User's email (requires users:read.email scope)
}

// QueryResponse represents a query response
type QueryResponse struct {
	Success bool   `json:"success"`
	Answer  string `json:"answer"`
	Message string `json:"message,omitempty"`
}

// StreamEvent represents an SSE event following the AGENT_REST_CONTRACT.
// EventType maps to the SSE "event:" field, Data is serialized as JSON in "data:".
type StreamEvent struct {
	EventType string // Named SSE event: session_id, content_delta, tool_start, tool_input, tool_result, end, error
	Data      any    // JSON-serializable payload
}
