package server

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/session"

	"knowledge-agent/internal/a2a"
	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/auth/keycloak"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// MaxRequestBodySize is the maximum allowed request body size (1MB)
// This prevents DoS attacks via large payloads
const MaxRequestBodySize = 1 << 20 // 1 MB

// AgentServer handles HTTP requests for the Knowledge Agent service
type AgentServer struct {
	agent          *agent.Agent
	config         *config.Config
	mux            *http.ServeMux
	rateLimiter    *RateLimiter
	a2aHandler     *A2AHandler
	readinessState *ReadinessState
	keycloakClient *keycloak.Client
}

// NewAgentServer creates a new HTTP server for the agent service
func NewAgentServer(agnt *agent.Agent, cfg *config.Config) *AgentServer {
	return NewAgentServerWithKeycloak(agnt, cfg, nil)
}

// NewAgentServerWithKeycloak creates a new HTTP server with Keycloak integration
// for looking up user groups when not available from JWT
func NewAgentServerWithKeycloak(agnt *agent.Agent, cfg *config.Config, keycloakClient *keycloak.Client) *AgentServer {
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
	// 5. ADK pre-processing (Langfuse trace, pre-search memory, session management)
	loopPreventionMiddleware := a2a.LoopPreventionMiddleware(&s.config.A2A)
	authMiddleware := AuthMiddlewareWithKeycloak(s.config, s.keycloakClient)
	membershipMiddleware := MembershipMiddleware(s.config)
	adkPreProcess := ADKPreProcessMiddleware(s.agent)

	// ADK REST endpoints: /agent/run and /agent/run_sse
	adkHandler := s.agent.RESTHandler()
	wrappedADK := adkPreProcess(adkHandler)

	s.mux.Handle("/agent/",
		http.StripPrefix("/agent",
			s.rateLimiter.Middleware()(loopPreventionMiddleware(authMiddleware(membershipMiddleware(wrappedADK))))))
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
