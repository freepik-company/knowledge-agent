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

// IngestRequest represents a thread ingestion request
type IngestRequest struct {
	SessionID string           `json:"session_id,omitempty"` // Optional: client-provided session ID (takes precedence over auto-generated)
	ThreadTS  string           `json:"thread_ts"`
	ChannelID string           `json:"channel_id"`
	Messages  []map[string]any `json:"messages"`
}

// IngestResponse represents an ingestion response
type IngestResponse struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	MemoriesAdded int    `json:"memories_added"`
}

// QueryRequest represents a question/query request
type QueryRequest struct {
	SessionID    string           `json:"session_id,omitempty"` // Optional: client-provided session ID (takes precedence over auto-generated)
	Question     string           `json:"question"`
	ThreadTS     string           `json:"thread_ts,omitempty"`
	ChannelID    string           `json:"channel_id,omitempty"`
	Messages     []map[string]any `json:"messages,omitempty"`       // Current thread context
	UserName     string           `json:"user_name,omitempty"`      // Slack @username
	UserRealName string           `json:"user_real_name,omitempty"` // User's real name
	UserEmail    string           `json:"user_email,omitempty"`     // User's email (requires users:read.email scope)
}

// QueryResponse represents a query response
type QueryResponse struct {
	Success bool   `json:"success"`
	Answer  string `json:"answer"`
	Message string `json:"message,omitempty"`
}
