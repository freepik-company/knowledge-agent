package server

import (
	"fmt"
	"net/http"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a"
	"google.golang.org/adk/session"

	a2ainternal "knowledge-agent/internal/a2a"
	"knowledge-agent/internal/logger"
)

// A2AConfig holds the configuration for the A2A handler
type A2AConfig struct {
	AgentURL       string          // Public URL for agent card (e.g., http://anton:8081)
	Agent          agent.Agent     // The ADK agent
	SessionService session.Service // Session service for the executor
}

// A2AHandler holds the HTTP handlers for A2A protocol
type A2AHandler struct {
	invokeHandler    http.Handler
	agentCardHandler http.Handler
	agentCard        *a2acore.AgentCard
}

// NewA2AHandler creates handlers for A2A protocol endpoints
func NewA2AHandler(cfg A2AConfig) (*A2AHandler, error) {
	log := logger.Get()

	// Validate required configuration
	if cfg.AgentURL == "" {
		return nil, fmt.Errorf("AgentURL is required for A2A handler")
	}
	if cfg.Agent == nil {
		return nil, fmt.Errorf("Agent is required for A2A handler")
	}
	if cfg.SessionService == nil {
		return nil, fmt.Errorf("SessionService is required for A2A handler")
	}

	// Build the public invocation URL
	invokeURL := cfg.AgentURL + "/a2a/invoke"

	// Build agent card from the agent
	agentCard := &a2acore.AgentCard{
		Name:               cfg.Agent.Name(),
		Description:        cfg.Agent.Description(),
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		URL:                invokeURL,
		PreferredTransport: a2acore.TransportProtocolJSONRPC,
		Skills:             adka2a.BuildAgentSkills(cfg.Agent),
		Capabilities:       a2acore.AgentCapabilities{Streaming: true},
	}

	log.Infow("Creating A2A handler",
		"agent_name", agentCard.Name,
		"invoke_url", invokeURL,
	)

	// Create the A2A executor with proper config (same pattern as ADK launcher)
	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        cfg.Agent.Name(),
			Agent:          cfg.Agent,
			SessionService: cfg.SessionService,
		},
	})

	// Create ownership-aware task store to prevent cross-user task access.
	// The default in-memory store does not enforce ownership, so any authenticated
	// user who knows a task UUID could read/cancel other users' tasks.
	taskStore := a2ainternal.NewOwnershipAwareTaskStore()

	// Create the request handler with:
	// - UserInterceptor: bridges our HTTP auth middleware identity to CallContext.User
	// - OwnershipAwareTaskStore: enforces task ownership on Get/Save
	reqHandler := a2asrv.NewHandler(executor,
		a2asrv.WithCallInterceptor(a2ainternal.NewUserInterceptor()),
		a2asrv.WithTaskStore(taskStore),
	)

	// Create the JSONRPC HTTP handler
	invokeHandler := a2asrv.NewJSONRPCHandler(reqHandler)

	// Create the agent card handler (static, publicly accessible)
	agentCardHandler := a2asrv.NewStaticAgentCardHandler(agentCard)

	return &A2AHandler{
		invokeHandler:    invokeHandler,
		agentCardHandler: agentCardHandler,
		agentCard:        agentCard,
	}, nil
}

// InvokeHandler returns the handler for /a2a/invoke
func (h *A2AHandler) InvokeHandler() http.Handler {
	return h.invokeHandler
}

// AgentCardHandler returns the handler for /.well-known/agent-card.json
func (h *A2AHandler) AgentCardHandler() http.Handler {
	return h.agentCardHandler
}

// AgentCard returns the agent card
func (h *A2AHandler) AgentCard() *a2acore.AgentCard {
	return h.agentCard
}
