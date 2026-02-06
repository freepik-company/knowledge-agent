package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/session"

	"knowledge-agent/internal/a2a"
	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/auth/keycloak"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/observability"
)

// MaxRequestBodySize is the maximum allowed request body size (1MB)
// This prevents DoS attacks via large payloads
const MaxRequestBodySize = 1 << 20 // 1 MB

// AgentInterface defines the interface for the agent
type AgentInterface interface {
	Query(ctx context.Context, req agent.QueryRequest) (*agent.QueryResponse, error)
	QueryStream(ctx context.Context, req agent.QueryRequest, onEvent func(agent.StreamEvent)) error
	Close() error
}

// A2AAgentProvider provides access to the underlying ADK agent for A2A protocol
type A2AAgentProvider interface {
	GetLLMAgent() adkagent.Agent
}

// AgentServer handles HTTP requests for the Knowledge Agent service
type AgentServer struct {
	agent          AgentInterface
	config         *config.Config
	mux            *http.ServeMux
	rateLimiter    *RateLimiter
	a2aHandler     *A2AHandler
	readinessState *ReadinessState
	keycloakClient *keycloak.Client
}

// NewAgentServer creates a new HTTP server for the agent service
func NewAgentServer(agnt AgentInterface, cfg *config.Config) *AgentServer {
	return NewAgentServerWithKeycloak(agnt, cfg, nil)
}

// NewAgentServerWithKeycloak creates a new HTTP server with Keycloak integration
// for looking up user groups when not available from JWT
func NewAgentServerWithKeycloak(agnt AgentInterface, cfg *config.Config, keycloakClient *keycloak.Client) *AgentServer {
	s := &AgentServer{
		agent:          agnt,
		config:         cfg,
		mux:            http.NewServeMux(),
		readinessState: NewReadinessState(),
		keycloakClient: keycloakClient,
	}

	// Register routes
	s.registerRoutes()

	return s
}

// SetReady marks the server as ready to accept traffic
func (s *AgentServer) SetReady() {
	s.readinessState.SetReady()
}

// SetNotReady marks the server as not ready (shutting down)
func (s *AgentServer) SetNotReady() {
	s.readinessState.SetNotReady()
}

// SetupA2A configures A2A protocol endpoints on this server
// This should be called after NewAgentServer if A2A is enabled
func (s *AgentServer) SetupA2A(llmAgent adkagent.Agent, sessionSvc session.Service, agentURL string) error {
	log := logger.Get()

	a2aHandler, err := NewA2AHandler(A2AConfig{
		AgentURL:       agentURL,
		Agent:          llmAgent,
		SessionService: sessionSvc,
	})
	if err != nil {
		return fmt.Errorf("failed to create A2A handler: %w", err)
	}

	s.a2aHandler = a2aHandler

	// Agent card is PUBLIC (no auth) - needed for agent discovery
	s.mux.Handle("/.well-known/agent-card.json", a2aHandler.AgentCardHandler())

	// A2A invoke is PROTECTED (with auth + membership middleware)
	loopPreventionMiddleware := a2a.LoopPreventionMiddleware(&s.config.A2A)
	authMiddleware := AuthMiddlewareWithKeycloak(s.config, s.keycloakClient)
	membershipMiddleware := MembershipMiddleware(s.config)

	s.mux.Handle("/a2a/invoke",
		s.rateLimiter.Middleware()(loopPreventionMiddleware(authMiddleware(membershipMiddleware(a2aHandler.InvokeHandler())))))

	log.Infow("A2A endpoints configured",
		"agent_card", "/.well-known/agent-card.json (public)",
		"invoke", "/a2a/invoke (authenticated)",
		"agent_url", agentURL,
	)

	return nil
}

// registerRoutes sets up all HTTP endpoints
func (s *AgentServer) registerRoutes() {
	// Public endpoints (no authentication)
	s.mux.HandleFunc("/health", HealthCheckHandler("knowledge-agent", ""))
	s.mux.HandleFunc("/ready", ReadinessHandler(s.readinessState)) // Kubernetes readiness probe
	s.mux.HandleFunc("/live", LivenessHandler())                   // Kubernetes liveness probe
	s.mux.Handle("/metrics", promhttp.Handler())                   // Prometheus metrics

	// Create rate limiter (10 requests/second, burst of 20)
	// TrustedProxies controls X-Forwarded-For handling - only trust it from configured proxies
	s.rateLimiter = NewRateLimiter(10.0, 20, s.config.Server.TrustedProxies)

	// Create middleware chain:
	// 1. Rate limiting (first, to prevent DoS)
	// 2. A2A loop prevention (before auth, to fail fast on loops)
	// 3. Authentication (identifies caller, extracts email/groups)
	// 4. Membership verification (requires user to be in allowed_emails/allowed_groups if enabled)
	// Note: keycloakClient enables group lookup from Keycloak when user has email but no JWT groups
	loopPreventionMiddleware := a2a.LoopPreventionMiddleware(&s.config.A2A)
	authMiddleware := AuthMiddlewareWithKeycloak(s.config, s.keycloakClient)
	membershipMiddleware := MembershipMiddleware(s.config)

	// API endpoints (protected with rate limiting, loop prevention, authentication, and membership)
	s.mux.Handle("/api/query",
		s.rateLimiter.Middleware()(loopPreventionMiddleware(authMiddleware(membershipMiddleware(http.HandlerFunc(s.handleQuery))))))

	// SSE streaming endpoint (same middleware chain)
	s.mux.Handle("/api/query/stream",
		s.rateLimiter.Middleware()(loopPreventionMiddleware(authMiddleware(membershipMiddleware(http.HandlerFunc(s.handleQueryStream))))))
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

// handleQuery handles query requests
func (s *AgentServer) handleQuery(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerID := ctxutil.CallerID(r.Context())

	log := logger.Get()

	// Limit request body size to prevent DoS
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	var req agent.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Check if error is due to body size limit
		if err.Error() == "http: request body too large" {
			log.Warnw("Request body too large", "caller", callerID, "max_size", MaxRequestBodySize)
			jsonError(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
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
		log.Errorw("Query error",
			"error", err,
			"caller", callerID,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		jsonError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Infow("Query completed",
		"caller", callerID,
		"duration_ms", time.Since(startTime).Milliseconds(),
		"success", true,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Errorw("Failed to encode response", "error", err)
	}
}

// handleQueryStream handles streaming query requests via SSE
func (s *AgentServer) handleQueryStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	callerID := ctxutil.CallerID(r.Context())
	log := logger.Get()

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	var req agent.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			log.Warnw("Request body too large", "caller", callerID, "max_size", MaxRequestBodySize)
			jsonError(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		log.Warnw("Invalid query request body", "error", err, "caller", callerID)
		jsonError(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Question == "" {
		log.Warnw("Missing question field", "caller", callerID)
		jsonError(w, "question is required", http.StatusBadRequest)
		return
	}

	log.Infow("Stream query request received",
		"caller", callerID,
		"question", req.Question,
		"channel_id", req.ChannelID,
	)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// SSE write callback
	writeSSE := func(event agent.StreamEvent) {
		data, err := json.Marshal(event)
		if err != nil {
			log.Errorw("Failed to marshal SSE event", "error", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	ctx := r.Context()
	if err := s.agent.QueryStream(ctx, req, writeSSE); err != nil {
		log.Errorw("Stream query error", "error", err, "caller", callerID)
		// Error event already emitted by QueryStream, but log it
	}
}

// handleMetricsJSON returns application metrics in JSON format (legacy endpoint)
// Deprecated: Use /metrics for Prometheus format
func (s *AgentServer) handleMetricsJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := observability.GetMetrics().GetStats()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log := logger.Get()
		log.Errorw("Failed to encode metrics", "error", err)
	}
}
