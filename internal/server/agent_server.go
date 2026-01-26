package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/metrics"
)

// AgentInterface defines the interface for the agent
type AgentInterface interface {
	IngestThread(ctx context.Context, req agent.IngestRequest) (*agent.IngestResponse, error)
	Query(ctx context.Context, req agent.QueryRequest) (*agent.QueryResponse, error)
	Close() error
}

// AgentServer handles HTTP requests for the Knowledge Agent service
type AgentServer struct {
	agent       AgentInterface
	config      *config.Config
	mux         *http.ServeMux
	rateLimiter *RateLimiter
}

// NewAgentServer creates a new HTTP server for the agent service
func NewAgentServer(agnt AgentInterface, cfg *config.Config) *AgentServer {
	s := &AgentServer{
		agent:  agnt,
		config: cfg,
		mux:    http.NewServeMux(),
	}

	// Register routes
	s.registerRoutes()

	return s
}

// registerRoutes sets up all HTTP endpoints
func (s *AgentServer) registerRoutes() {
	// Public endpoints (no authentication)
	s.mux.HandleFunc("/health", HealthCheckHandler("knowledge-agent", ""))
	s.mux.Handle("/metrics", promhttp.Handler()) // Prometheus metrics

	// Create rate limiter (10 requests/second, burst of 20)
	s.rateLimiter = NewRateLimiter(10.0, 20)

	// Create authentication middleware
	authMiddleware := AuthMiddleware(s.config)

	// API endpoints (protected with rate limiting and authentication)
	s.mux.Handle("/api/ingest-thread",
		s.rateLimiter.Middleware()(authMiddleware(http.HandlerFunc(s.handleIngestThread))))
	s.mux.Handle("/api/query",
		s.rateLimiter.Middleware()(authMiddleware(http.HandlerFunc(s.handleQuery))))
}

// Close stops the rate limiter cleanup routine
func (s *AgentServer) Close() error {
	if s.rateLimiter != nil {
		s.rateLimiter.Close()
	}
	return nil
}

// Handler returns the HTTP handler
func (s *AgentServer) Handler() http.Handler {
	return s.mux
}

// handleIngestThread handles thread ingestion requests
func (s *AgentServer) handleIngestThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerID := ctxutil.CallerID(r.Context())
	log := logger.Get()

	var req agent.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warnw("Invalid ingest request body", "error", err, "caller", callerID)
		jsonError(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ThreadTS == "" || req.ChannelID == "" {
		log.Warnw("Missing required fields", "caller", callerID)
		jsonError(w, "thread_ts and channel_id are required", http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		log.Warnw("Empty messages array", "caller", callerID)
		jsonError(w, "messages array cannot be empty", http.StatusBadRequest)
		return
	}

	log.Infow("IngestThread request received",
		"caller", callerID,
		"thread_ts", req.ThreadTS,
		"channel_id", req.ChannelID,
		"message_count", len(req.Messages),
	)

	ctx := r.Context()
	resp, err := s.agent.IngestThread(ctx, req)
	if err != nil {
		log.Errorw("Ingest error", "error", err, "caller", callerID)
		jsonError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Errorw("Failed to encode response", "error", err)
	}
}

// handleQuery handles query requests
func (s *AgentServer) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerID := ctxutil.CallerID(r.Context())

	log := logger.Get()

	var req agent.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warnw("Invalid query request body", "error", err, "caller", callerID)
		jsonError(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Question == "" {
		log.Warnw("Missing question field", "caller", callerID)
		jsonError(w, "question is required", http.StatusBadRequest)
		return
	}

	logFields := []any{
		"caller", callerID,
		"question", req.Question,
		"channel_id", req.ChannelID,
	}
	if req.UserName != "" {
		logFields = append(logFields, "user_name", req.UserName)
	}
	log.Infow("Query request received", logFields...)

	ctx := r.Context()
	resp, err := s.agent.Query(ctx, req)
	if err != nil {
		log.Errorw("Query error", "error", err, "caller", callerID)
		jsonError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Errorw("Failed to encode response", "error", err)
	}
}

// handleMetricsJSON returns application metrics in JSON format (legacy endpoint)
// Deprecated: Use /metrics for Prometheus format
func (s *AgentServer) handleMetricsJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := metrics.Get().GetStats()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log := logger.Get()
		log.Errorw("Failed to encode metrics", "error", err)
	}
}
