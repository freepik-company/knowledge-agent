package a2a

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/remoteagent"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// CreateSubAgents creates remote ADK agents from configuration using remoteagent.NewA2A
// These agents can be used as sub-agents in the main LLM agent
func CreateSubAgents(cfg *config.A2AConfig) ([]agent.Agent, error) {
	log := logger.Get()

	if !cfg.Enabled {
		return nil, nil
	}

	if len(cfg.SubAgents) == 0 {
		log.Debug("A2A enabled but no sub_agents configured")
		return nil, nil
	}

	log.Infow("Creating A2A sub-agents",
		"self_name", cfg.SelfName,
		"sub_agents_count", len(cfg.SubAgents),
		"polling", cfg.Polling,
	)

	var subAgents []agent.Agent

	for _, subAgentCfg := range cfg.SubAgents {
		remoteAgent, err := createRemoteAgent(subAgentCfg, cfg.Polling)
		if err != nil {
			// Graceful degradation: log warning but continue with other agents
			log.Warnw("Failed to create remote agent, skipping",
				"agent", subAgentCfg.Name,
				"error", err,
			)
			continue
		}

		subAgents = append(subAgents, remoteAgent)
		log.Infow("Remote sub-agent created",
			"name", subAgentCfg.Name,
			"endpoint", subAgentCfg.Endpoint,
			"auth_type", subAgentCfg.Auth.Type,
			"polling", cfg.Polling,
		)
	}

	if len(subAgents) > 0 {
		log.Infow("A2A sub-agents created successfully",
			"count", len(subAgents),
		)
	} else if len(cfg.SubAgents) > 0 {
		log.Warn("A2A enabled but no sub-agents were created successfully")
	}

	return subAgents, nil
}

// createRemoteAgent creates a single remote agent using ADK's remoteagent package
func createRemoteAgent(cfg config.A2ASubAgentConfig, polling bool) (agent.Agent, error) {
	log := logger.Get()

	log.Debugw("Creating remote agent",
		"name", cfg.Name,
		"endpoint", cfg.Endpoint,
		"description", cfg.Description,
		"auth_type", cfg.Auth.Type,
		"polling", polling,
	)

	// Prepare auth headers if needed
	var authHeaderName, authHeaderValue string
	authType := strings.ToLower(cfg.Auth.Type)
	if authType != "" && authType != "none" {
		var err error
		authHeaderName, authHeaderValue, err = resolveAuthHeader(cfg.Auth)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve auth for agent %s: %w", cfg.Name, err)
		}

		log.Debugw("Configuring auth for sub-agent",
			"agent", cfg.Name,
			"auth_type", authType,
			"header", authHeaderName,
		)
	}

	// Build card resolve options (for pre-resolving agent card)
	var cardResolveOpts []agentcard.ResolveOption
	if authHeaderName != "" {
		cardResolveOpts = append(cardResolveOpts, agentcard.WithRequestHeader(authHeaderName, authHeaderValue))
	}

	// Pre-resolve the agent card so we can modify capabilities
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	card, err := agentcard.DefaultResolver.Resolve(ctx, cfg.Endpoint, cardResolveOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agent card for %s: %w", cfg.Name, err)
	}

	// Disable streaming when polling is enabled (required for A2A communication)
	// Without this, large responses can timeout or cause connection issues
	if polling {
		log.Debugw("Disabling streaming for sub-agent (polling mode)",
			"agent", cfg.Name,
			"original_streaming", card.Capabilities.Streaming,
		)
		card.Capabilities.Streaming = false
	}

	// Build client factory options
	factoryOpts := []a2aclient.FactoryOption{
		a2aclient.WithConfig(a2aclient.Config{
			Polling: polling,
		}),
	}

	// Add auth interceptor if configured
	if authHeaderName != "" {
		factoryOpts = append(factoryOpts, a2aclient.WithInterceptors(&authInterceptor{
			headerName:  authHeaderName,
			headerValue: authHeaderValue,
		}))
	}

	// Build A2A config with pre-resolved card
	a2aCfg := remoteagent.A2AConfig{
		Name:          cfg.Name,
		Description:   cfg.Description,
		AgentCard:     card, // Use pre-resolved card (with streaming disabled if polling)
		ClientFactory: a2aclient.NewFactory(factoryOpts...),
	}

	// Create remote agent
	remoteAgent, err := remoteagent.NewA2A(a2aCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create remote agent %s: %w", cfg.Name, err)
	}

	return remoteAgent, nil
}

// resolveAuthHeader resolves the auth configuration to header name and value
func resolveAuthHeader(auth config.A2AAuthConfig) (headerName, headerValue string, err error) {
	authType := strings.ToLower(auth.Type)

	switch authType {
	case "api_key":
		// API Key auth: custom header with key from environment
		if auth.Header == "" {
			return "", "", fmt.Errorf("api_key auth requires 'header' field")
		}
		if auth.KeyEnv == "" {
			return "", "", fmt.Errorf("api_key auth requires 'key_env' field")
		}
		key := os.Getenv(auth.KeyEnv)
		if key == "" {
			return "", "", fmt.Errorf("environment variable %s not set", auth.KeyEnv)
		}
		return auth.Header, key, nil

	case "bearer":
		// Bearer token auth: Authorization header
		if auth.TokenEnv == "" {
			return "", "", fmt.Errorf("bearer auth requires 'token_env' field")
		}
		token := os.Getenv(auth.TokenEnv)
		if token == "" {
			return "", "", fmt.Errorf("environment variable %s not set", auth.TokenEnv)
		}
		return "Authorization", "Bearer " + token, nil

	case "oauth2":
		// OAuth2 is more complex - not yet supported for sub_agents
		// Use legacy a2a.agents config for OAuth2
		return "", "", fmt.Errorf("oauth2 auth not yet supported for sub_agents, use legacy a2a.agents config")

	default:
		return "", "", fmt.Errorf("unsupported auth type: %s", authType)
	}
}

// authInterceptor implements a2aclient.CallInterceptor to add auth headers to requests
type authInterceptor struct {
	a2aclient.PassthroughInterceptor
	headerName  string
	headerValue string
}

// Before adds the auth header to the request metadata
func (ai *authInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	if req.Meta == nil {
		req.Meta = make(a2aclient.CallMeta)
	}
	req.Meta[ai.headerName] = []string{ai.headerValue}
	return ctx, nil
}
