package server

import (
	"net/http"

	"knowledge-agent/internal/slack"
)

// SlackServer handles HTTP requests for the Slack Bot service (webhook mode)
type SlackServer struct {
	handler *slack.Handler
	mux     *http.ServeMux
}

// NewSlackServer creates a new HTTP server for the Slack bot service
func NewSlackServer(handler *slack.Handler) *SlackServer {
	s := &SlackServer{
		handler: handler,
		mux:     http.NewServeMux(),
	}

	// Register routes
	s.registerRoutes()

	return s
}

// registerRoutes sets up all HTTP endpoints
func (s *SlackServer) registerRoutes() {
	// Health check
	s.mux.HandleFunc("/health", HealthCheckHandler("slack-bot", "webhook"))

	// Slack events endpoint
	s.mux.HandleFunc("/slack/events", s.handler.HandleEvents)
}

// Handler returns the HTTP handler
func (s *SlackServer) Handler() http.Handler {
	return s.mux
}
